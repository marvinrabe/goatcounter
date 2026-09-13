package httpx

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
)

func TestUserErrors(t *testing.T) {
	secret := errors.New("database password=secret")
	code, err := UserError(secret)
	if code != 500 || err.Error() == secret.Error() {
		t.Fatalf("internal detail exposed: %d %v", code, err)
	}
	code, _ = UserError(fmt.Errorf("wrapped: %w", context.DeadlineExceeded))
	if code != http.StatusGatewayTimeout {
		t.Fatalf("deadline status = %d", code)
	}
	cause := errors.New("bad date")
	wrapped := Errorf(400, "invalid input: %w", cause)
	if !errors.Is(wrapped, cause) {
		t.Fatal("HTTP error lost its cause")
	}
	code, err = UserError(wrapped)
	if code != 400 || err.Error() != "invalid input: bad date" {
		t.Fatalf("client message = %d %v", code, err)
	}
}
