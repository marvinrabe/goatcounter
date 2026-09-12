package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/marvinrabe/goatcounter"
	"github.com/marvinrabe/goatcounter/internal/log"
	"zgo.at/errors"
	"zgo.at/guru"
	"zgo.at/zdb"
	"zgo.at/zdb/drivers"
	"zgo.at/zli"
	"zgo.at/zvalidate"
)

const helpDB = `
The db command manages the GoatCounter database.

Some common examples:

    Create a user to log in with:

        $ goatcounter db create user -email martin@example.com

        GoatCounter tracks a single site; the site itself is created
        automatically on first run. Any username works when logging in; only
        the password is checked.


    Run database migrations:

        $ goatcounter db migrate all

` + helpDBCommands + `

Flags accepted by all commands:

  -db          Database connection: "sqlite+<file>"
               See "goatcounter help db" for detailed documentation. Default:
               sqlite+./goatcounter-data/db.sqlite3

  -createdb    Create the database if it doesn't exist yet.

  -debug       Modules to debug, comma-separated or 'all' for all modules.
               See "goatcounter help debug" for a list of modules.

show command:

    -find       User to find; you can use the numeric ID column (e.g. 1) or the
                email address ("user@example.com").

    -format     Format to print, accepted values:

                    table     ASCII table, one row per line.
                    vertical  Vertical table, one column per line (default).
                    csv       CSV (includes header).
                    json      JSON, as an array of objects.
                    html      HTML table.

delete command:

    -find       As documented in show

    -force      Force deletion even if this is the last user.


create and update commands:

    The create and update commands accept a set of flags with column values.
    You can't set all columns, just the useful ones for regular management. The
    flags for create and update are identical, except that "update" also needs
    a -find flag; this is documented above in the show command.

    You may need to restart GoatCounter for some changes to take effect due to
    caching.

    You can add multiple -find flags to update multiple rows.

    Flags marked with * are required for create; for update only the flags that
    are given are updated.

    Flags for "user":

        -email*     Email address; required to log in.

        -password   Password; will be asked interactively if omitted. Read from
                    stdin if it's "-".

migrate command:

    Run or print database migrations.

        -dev        Load migrations from filesystem, rather than using the
                    migrations compiled in the binary.

        -test       Rollback migration after running the migrations instead of
                    committing it. Useful to test if migrations will run
                    correctly without actually altering the database.

        -show       Only show the SQL it would execute, but don't run anything.

    Positional arguments are names of the migration, either as just the name
    ("2020-01-05-2-x") or as the file path ("./db/migrate/2020-01-05-2-x.sql").

    Special values:

        all         Run all pending migrations.
        pending     Show pending migrations but do not run anything. Exits with
                    1 if there are pending migrations, or 0 if there aren't.
        list        List all migrations; pending migrations are prefixed with
                    "pending: ". Always exits with 0.

    Note: you can also use -automigrate flag for the serve command to run
    migrations on startup.

newdb command:

    Create a new database. This is the same what "goatcounter serve" or
    "goatcounter db -createdb [command]" do if no database exists yet.

    Exits with 0 if the database was already created, 2 if the database already
    exists (integrity isn't checked, just existence), or 1 on any other error.

schema-sqlite command:

    Print the compiled-in database schema for SQLite, in case you want to
    create the database manually from the schema.

test command:

    Test if the database exists; exits with 0 on success, 2 if the database
    doesn't exist, and 1 on any other error.

    This is useful for setting up new databases in scripts if you don't want to
    use the default database creation; e.g.:

        goatcounter db test -db [..]
        if [ $? -eq 2 ]; then
            goatcounter db newdb -db [..]
        fi

query command:

    Run a query against the database, this is unrestricted and can modify or
    delete anything, so use with care. Can be useful in cases where you don't
    have a sqlite3 CLI available.

    Only runs one query, unless -format=exec is given.

    -format         Format to print, accepted values:

                        table     ASCII table, one row per line (default).
                        vertical  Vertical table, one column per line.
                        csv       CSV (includes header).
                        json      JSON, as an array of objects.
                        html      HTML table.
                        exec      Execute the query but don't return result.
                                  Mostly useful for multiple "create table"
                                  queries.

    There are a few special values to get some data:

        screensize      Get some aggregate data about screen sizes.
        ua              List all User-Agent headers, but not bots.
        bots            List all User-Agent headers that are a "bot".
        unknown-ua      List all User-Agent headers that do not have full
                        browser/system associated with them.

Detailed documentation on the -db flag:

    GoatCounter uses SQLite. All commands accept the -db flag to customize the
    database connection string.

    The database is automatically created for the "serve" command, but you need
    to add -createdb to any other commands to create the database. This is to
    prevent accidentally operating on the wrong (new) database.

SQLite notes:

    This is the default database engine as it has no dependencies, and for most
    small to medium usage it should be more than fast enough.

    The SQLite connection string is usually a filename, optionally prefixed
    with "file:". Parameters can be added as a URL query string after a ?:

        -db 'sqlite+mydb.sqlite?param=value&other=value'

    See the go-sqlite3 documentation for a list of supported parameters:
    https://github.com/mattn/go-sqlite3/#connection-string

    A few parameters are different from the SQLite defaults:

        _journal_mode=wal          Usually faster with better concurrency,
                                   with little drawbacks for most use cases.
        _busy_timeout=200          Wait 200ms for locks instead of immediately
                                   throwing an error.
        _cache_size=-4000          4M cache size, instead of 2M. Note this is
                                   per connection, so it multiplies by -dbconn.

    But you can change them if you wish; for example to use the SQLite defaults:

        -db 'sqlite+mydb.sqlite?_journal_mode=delete&_busy_timeout=0&_cache_size=-2000'

`

const helpDBCommands = `List of commands:

     create user        Create a new user.
     update user        Update a user.
     delete user        Delete a user.
     show   user        Show a user.

     newdb              Create a new database.
     migrate            Run or view database migrations.
     schema-sqlite      Print the SQLite schema.
     test               Test if the database exists.
     query              Run a query.`

const helpDBShort = "\n" + helpDBCommands + `

Use "goatcounter help db" for the full documentation.`

type (
	findMany interface {
		Find(context.Context, []string) error
		IDs() []int32
		Delete(context.Context, bool) error
	}
	stringFlag interface {
		String() string
		Set() bool
	}
)

func cmdDB(f zli.Flags, ready chan<- struct{}, stop chan struct{}) error {
	defer func() { ready <- struct{}{} }()

	var (
		dbConnect = f.String(defaultDB(), "db").Pointer()
		debug     = f.StringList(nil, "debug")
		createdb  = f.Bool(false, "createdb").Pointer()
	)

start:
	cmd, err := f.ShiftCommand()
	if err != nil && !errors.Is(err, zli.ErrCommandNoneGiven{}) {
		return err
	}

	switch cmd {
	default:
		// Be forgiving if someone reverses the order of "create" and "site".
		maybeCmd := f.Shift()
		if slices.Contains([]string{"create", "update", "delete", "show"}, maybeCmd) {
			f.Args = append([]string{maybeCmd, cmd}, f.Args...)
			goto start
		}

		return errors.Errorf("unknown command for \"db\": %q\n%s", cmd, helpDBShort)
	case "": //, zli.CommandNoneGiven:
		return errors.New("\"db\" needs a subcommand\n" + helpDBShort)
	case "help":
		zli.WantColor = true
		printHelp(helpDB)
		return nil

	case "schema-sqlite":
		return cmdDBSchema(cmd)
	case "test":
		return cmdDBTest(f, dbConnect, debug.StringsSplit(","), true)
	case "migrate":
		return cmdDBMigrate(f, dbConnect, debug.StringsSplit(","), createdb)
	case "query":
		return cmdDBQuery(f, dbConnect, debug.StringsSplit(","), createdb)
	case "show":
		return cmdDBShow(f, cmd, dbConnect, debug.StringsSplit(","), createdb)
	case "delete":
		return cmdDBDelete(f, cmd, dbConnect, debug.StringsSplit(","), createdb)

	case "create", "update":
		if _, err := getTable(&f, cmd); err != nil {
			return err
		}
		return cmdDBUser(f, cmd, dbConnect, debug.StringsSplit(","), createdb)

	case "newdb":
		err := cmdDBTest(f, dbConnect, debug.StringsSplit(","), false)
		if err == nil {
			return guru.Errorf(2, "database at %q already exists", *dbConnect)
		}

		var cErr *drivers.NotExistError
		if !errors.As(err, &cErr) {
			return err
		}

		db, _, err := connectDB(*dbConnect, "", []string{"pending"}, true, false)
		if err != nil {
			return err
		}
		return db.Close()
	}
}

func getTable(f *zli.Flags, cmd string) (string, error) {
	tbl, err := f.ShiftCommand()
	if err != nil && !errors.Is(err, zli.ErrCommandNoneGiven{}) {
		return "", err
	}

	switch tbl {
	default:
		return "", errors.Errorf("unknown table %q\n%s", tbl, helpDBShort)
	case "":
		return "", errors.Errorf("%q commands needs a table name\n%s", cmd, helpDBShort)
	case "help":
		zli.WantColor = true
		printHelp(helpDB)
		return "", guru.New(0, "")

	case "user", "users":
		return "user", nil
	}
}

func getFormat(format string) (zdb.DumpArg, error) {
	switch format {
	case "table":
		return 0, nil
	case "vertical":
		return zdb.DumpVertical, nil
	case "csv":
		return zdb.DumpCSV, nil
	case "json":
		return zdb.DumpJSON, nil
	case "html":
		return zdb.DumpHTML, nil
	case "exec":
		return -1, nil
	default:
		return 0, fmt.Errorf("-format: unknown value: %q", format)
	}
}

func cmdDBSchema(cmd string) error {
	d, err := goatcounter.DB.ReadFile("db/schema.gotxt")
	if err != nil {
		return err
	}
	d, err = zdb.Template(zdb.DialectSQLite, string(d))
	if err != nil {
		return err
	}
	fmt.Fprint(zli.Stdout, string(d))
	return nil
}

func cmdDBTest(f zli.Flags, dbConnect *string, debug []string, print bool) error {
	if err := f.Parse(zli.FromEnv("GOATCOUNTER")); err != nil && !errors.As(err, &zli.ErrUnknownEnv{}) {
		return err
	}

	if *dbConnect == "" {
		return errors.New("must add -db flag")
	}
	log.SetDebug(debug)
	db, err := zdb.Connect(context.Background(), zdb.ConnectOptions{Connect: *dbConnect})
	if err != nil {
		return err
	}
	defer db.Close()

	ctx := zdb.WithDB(context.Background(), db)

	info, err := db.Info(ctx)
	if err != nil {
		return err
	}

	var i int
	err = db.Get(ctx, &i, `select 1 from version`)
	if err != nil {
		return fmt.Errorf("select 1 from version: %w", err)
	}
	if print {
		fmt.Fprintf(zli.Stdout, "DB at %q seems okay; %s version %s\n",
			*dbConnect, info.DriverName, info.Version)
	}
	return nil
}

func cmdDBQuery(f zli.Flags, dbConnect *string, debug []string, createdb *bool) error {
	var (
		format = f.String("table", "format")
	)
	if err := f.Parse(zli.FromEnv("GOATCOUNTER")); err != nil && !errors.As(err, &zli.ErrUnknownEnv{}) {
		return err
	}

	query, err := zli.InputOrArgs(f.Args, " ", false)
	if err != nil {
		return err
	}
	if len(query) == 0 || query[0] == "" {
		return errors.New("need a query")
	}

	log.SetDebug(debug)

	db, ctx, err := connectDB(*dbConnect, "", nil, *createdb, false)
	if err != nil {
		return err
	}
	defer db.Close()

	q, _, err := zdb.Load(db, "db.query."+query[0]+".sql")
	if err != nil {
		q = strings.Join(query, " ")
	}

	dump, err := getFormat(format.String())
	if err != nil {
		return err
	}

	if dump == -1 {
		return zdb.Exec(ctx, q)
	}
	zdb.Dump(ctx, zli.Stdout, q, dump)
	return nil
}

func getManyFinder(ctx context.Context, f *zli.Flags, cmd string, find []string) (findMany, string, error) {
	if len(find) == 0 {
		return nil, "", errors.New("need at least on -find flag")
	}

	tbl, err := getTable(f, cmd)
	if err != nil {
		return nil, "", err
	}

	finder := map[string]findMany{
		"user": &goatcounter.Users{},
	}[tbl]

	err = finder.Find(ctx, find)
	return finder, tbl, err
}

func dbParseFlag(f zli.Flags, dbConnect *string, debug []string, createdb *bool) (zdb.DB, context.Context, error) {
	if err := f.Parse(zli.FromEnv("GOATCOUNTER")); err != nil && !errors.As(err, &zli.ErrUnknownEnv{}) {
		return nil, nil, err
	}
	log.SetDebug(debug)

	db, _, err := connectDB(*dbConnect, "", []string{"pending"}, *createdb, false)
	if err != nil {
		return nil, nil, err
	}

	ctx := goatcounter.NewContext(context.Background(), db)
	return db, ctx, nil
}

func cmdDBShow(f zli.Flags, cmd string, dbConnect *string, debug []string, createdb *bool) error {
	var (
		find   = f.StringList(nil, "find")
		format = f.String("vertical", "format")
	)
	db, ctx, err := dbParseFlag(f, dbConnect, debug, createdb)
	if err != nil {
		return err
	}
	defer db.Close()

	finder, tbl, err := getManyFinder(ctx, &f, cmd, find.Strings())
	if err != nil {
		return err
	}

	q := map[string]string{
		"user": "users where user_id",
	}[tbl]

	ids := finder.IDs()
	if len(ids) == 0 {
		return errors.New("nothing found")
	}

	dump, err := getFormat(format.String())
	if err != nil {
		return err
	}

	zdb.Dump(ctx, zli.Stdout, `select * from `+q+` in (?)`, ids, dump)
	return nil
}

func cmdDBDelete(f zli.Flags, cmd string, dbConnect *string, debug []string, createdb *bool) error {
	var (
		find  = f.StringList(nil, "find")
		force = f.Bool(false, "force").Pointer()
	)
	db, ctx, err := dbParseFlag(f, dbConnect, debug, createdb)
	if err != nil {
		return err
	}
	defer db.Close()

	finder, _, err := getManyFinder(ctx, &f, cmd, find.Strings())
	if err != nil {
		return err
	}
	return finder.Delete(ctx, *force)
}

func cmdDBUser(f zli.Flags, cmd string, dbConnect *string, debug []string, createdb *bool) error {
	var (
		email = f.String("", "email")
		pwd   = f.String("", "password")
		find  *[]string
	)
	if cmd == "update" {
		find = f.StringList(nil, "find").Pointer()
	}
	db, ctx, err := dbParseFlag(f, dbConnect, debug, createdb)
	if err != nil {
		return err
	}
	defer db.Close()

	if cmd == "create" {
		return cmdDBUserCreate(ctx, email.String(), pwd.String())
	}
	return cmdDBUserUpdate(ctx, *find, email, pwd)
}

func cmdDBUserCreate(ctx context.Context, email, pwd string) error {
	v := zvalidate.New()
	v.Required("-email", email)
	v.Email("-email", email)
	if v.HasErrors() {
		return v
	}

	var site goatcounter.Site
	err := site.Load(ctx)
	if err != nil {
		return err
	}
	ctx = goatcounter.WithSite(ctx, &site)

	pwd, err = readPassword(pwd)
	if err != nil {
		return err
	}

	return (&goatcounter.User{
		Email:    email,
		Password: []byte(pwd),
	}).Insert(ctx, false)
}

func cmdDBUserUpdate(ctx context.Context, find []string, email, pwd stringFlag) error {
	v := zvalidate.New()
	v.Required("-find", find)
	v.Email("-email", email.String())
	if v.HasErrors() {
		return v
	}

	var users goatcounter.Users
	err := users.Find(ctx, find)
	if err != nil {
		return err
	}

	return zdb.TX(ctx, func(ctx context.Context) error {
		for _, u := range users {
			if email.Set() {
				u.Email = email.String()
				if err := u.Update(ctx, true); err != nil {
					return err
				}
			}
			if pwd.Set() {
				if err := u.UpdatePassword(ctx, pwd.String()); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// readPassword reads the password from stdin if it's "-", or asks for it
// interactively if it's empty.
func readPassword(pwd string) (string, error) {
	if pwd == "-" {
		p, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("reading password: %w", err)
		}
		return string(p), nil
	}
	if pwd == "" {
		return zli.AskPassword(8)
	}
	return pwd, nil
}
