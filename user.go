package goatcounter

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/marvinrabe/goatcounter/internal/i18n"
	"golang.org/x/crypto/bcrypt"
	"zgo.at/errors"
	"zgo.at/guru"
	"zgo.at/zdb"
	"zgo.at/zstd/zstrconv"
	"zgo.at/zstd/ztime"
	"zgo.at/zvalidate"
)

type UserID int32

type User struct {
	ID       UserID `db:"user_id,id" json:"id,readonly"`
	Email    string `db:"email" json:"email"`
	Password []byte `db:"password" json:"-"`

	CreatedAt time.Time  `db:"created_at,readonly" json:"created_at,readonly"`
	UpdatedAt *time.Time `db:"updated_at" json:"updated_at,readonly"`
}

func (User) Table() string { return "users" }

var _ zdb.Defaulter = &User{}

func (u *User) Defaults(ctx context.Context) {
	if u.CreatedAt.IsZero() {
		u.CreatedAt = ztime.Now(ctx)
	} else {
		u.UpdatedAt = new(ztime.Now(ctx))
	}
}

var _ zdb.Validator = &User{}

func (u *User) Validate(ctx context.Context) error {
	v := NewValidate(ctx)

	v.Required("email", u.Email)
	v.Len("email", u.Email, 5, 255)
	v.Email("email", u.Email)

	return v.ErrorOrNil()
}

// Hash the password, replacing the plain-text one.
func (u *User) hashPassword(ctx context.Context) error {
	// Length is capped to 50 characters in Validate.
	if len(u.Password) > 50 {
		return errors.Errorf("User.hashPassword: already hashed")
	}

	v := zvalidate.New()
	v.Required("password", u.Password)
	v.UTF8("password", string(u.Password))
	if len(u.Password) < 8 || len(u.Password) > 50 {
		v.Append("password", "must be between 8 and 50 bytes")
	}
	if v.HasErrors() {
		return v
	}

	cost := bcrypt.DefaultCost
	if Config(ctx).BcryptMinCost { // Otherwise every test take 1.5s extra
		cost = bcrypt.MinCost
	}
	pwd, err := bcrypt.GenerateFromPassword(u.Password, cost)
	if err != nil {
		return errors.Errorf("User.hashPassword: %w", err)
	}
	u.Password = pwd
	return nil
}

// Insert a new row.
func (u *User) Insert(ctx context.Context, allowBlankPassword bool) error {
	if !(u.Password == nil && allowBlankPassword) {
		err := u.hashPassword(ctx)
		if err != nil {
			return errors.Wrap(err, "User.Insert")
		}
	}

	err := zdb.Insert(ctx, u)
	if zdb.ErrUnique(err) {
		err = guru.New(400, i18n.T(ctx, `error/email-exists|
			The email address “%(email)” is already used by another user`, u.Email))
	}
	return errors.Wrap(err, "User.Insert")
}

// Delete this user.
func (u *User) Delete(ctx context.Context, lastUser bool) error {
	if !lastUser {
		var users Users
		err := users.List(ctx)
		if err != nil {
			return errors.Wrap(err, "User.Delete")
		}
		if len(users) == 1 && users[0].ID == u.ID {
			return errors.New("can't delete the last user")
		}
	}

	err := zdb.Exec(ctx, `delete from users where user_id=?`, u.ID)
	return errors.Wrap(err, "User.Delete")
}

// Update this user's email and settings.
func (u *User) Update(ctx context.Context, emailChanged bool) error {
	err := zdb.Update(ctx, u, zdb.UpdateAll)
	if zdb.ErrUnique(err) {
		err = guru.New(400, i18n.T(ctx, `error/email-exists|
			The email address “%(email)” is already used by another user`, u.Email))
	}
	return errors.Wrap(err, "User.Update")
}

// UpdatePassword updates this user's password.
func (u *User) UpdatePassword(ctx context.Context, pwd string) error {
	u.Password = []byte(pwd)

	err := u.hashPassword(ctx)
	if err != nil {
		return errors.Wrap(err, "User.UpdatePassword")
	}

	err = zdb.Update(ctx, u, "password", "updated_at")
	return errors.Wrap(err, "User.UpdatePassword")
}

// CorrectPassword verifies that this password is correct.
func (u User) CorrectPassword(pwd string) (bool, error) {
	err := bcrypt.CompareHashAndPassword(u.Password, []byte(pwd))
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return false, nil
	}
	if err != nil {
		return false, errors.Errorf("user.CorrectPassword: %w", err)
	}
	return true, nil
}

// ByID gets a user by id.
func (u *User) ByID(ctx context.Context, id UserID) error {
	err := zdb.Get(ctx, u, `select * from users where user_id=?`, id)
	return errors.Wrap(err, "User.ByID")
}

// ByEmail gets a user by email address.
func (u *User) ByEmail(ctx context.Context, email string) error {
	err := zdb.Get(ctx, u, `select * from users where lower(email) = lower(?)`, email)
	return errors.Wrapf(err, "User.ByEmail(%q)", email)
}

// Find a user: by ID if ident is a number, or by email if it's not.
func (u *User) Find(ctx context.Context, ident string) error {
	id, err := zstrconv.ParseInt[UserID](ident, 10)
	if err == nil {
		return errors.Wrapf(u.ByID(ctx, id), "User.Find(%q)", ident)
	}
	return errors.Wrapf(u.ByEmail(ctx, ident), "User.Find(%q)", ident)
}

func (u User) EmailShort() string {
	local, _, ok := strings.Cut(u.Email, "@")
	if ok {
		return local + "@"
	}
	return u.Email
}

type Users []User

// List all users.
func (u *Users) List(ctx context.Context) error {
	err := zdb.Select(ctx, u, `select * from users order by user_id asc`)
	return errors.Wrap(err, "Users.List")
}

// Find users: by ID if ident is a number, or by email if it's not.
func (u *Users) Find(ctx context.Context, ident []string) error {
	ids, strs := splitIntStr(ident)
	err := zdb.Select(ctx, u, `select * from users where
		{{:ids user_id in (:ids) or}}
		{{:strs! 0=1}}
		{{:strs email in (:strs)}}`,
		map[string]any{"ids": ids, "strs": strs})
	return errors.Wrap(err, "Users.Find")
}

// IDs gets a list of all IDs for these users.
func (u *Users) IDs() []int32 {
	ids := make([]int32, 0, len(*u))
	for _, uu := range *u {
		ids = append(ids, int32(uu.ID))
	}
	return ids
}

// Delete all users in this selection.
func (u *Users) Delete(ctx context.Context, force bool) error {
	err := zdb.TX(ctx, func(ctx context.Context) error {
		for _, uu := range *u {
			err := uu.Delete(ctx, force)
			if err != nil {
				return err
			}
		}
		return nil
	})
	return errors.Wrap(err, "Users.Delete")
}

func splitIntStr(ident []string) ([]int64, []string) {
	var (
		ids  []int64
		strs []string
	)
	for _, i := range ident {
		id, err := strconv.ParseInt(i, 10, 64)
		if err == nil {
			ids = append(ids, id)
		} else {
			strs = append(strs, i)
		}
	}
	return ids, strs
}
