package doctor

import (
	"context"
	"testing"
)

func TestDefaultDoctorDoesNotValidateAuthentication(t *testing.T) {
	result := authenticationCheck(context.Background(), nil)
	if result.Status != "unchecked" {
		t.Fatalf("unverified credentials reported as valid: %+v", result)
	}
}
