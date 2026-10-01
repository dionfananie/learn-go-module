package utility

import (
	"encoding/json"
	"fmt"
	"learn-go/src/service"
	"net/http"

	"github.com/go-playground/validator/v10"
)

func JSONError(w http.ResponseWriter, status int, m string) {
	w.Header().Set("Content-type", "application/json")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": m,
	})

}

func BindMessage(ve validator.ValidationErrors) map[string]string {
	msgs := make(map[string]string, len(ve))
	for _, fe := range ve {
		switch fe.Field() {
		case "Name":
			msgs[fe.Field()] = fieldMessage(fe, "name", service.ErrUserNameRequired)
		case "PhoneNumber":
			msgs[fe.Field()] = fieldMessage(fe, "phone number", service.ErrUserPhoneRequired)
		case "Password":
			msgs[fe.Field()] = fieldMessage(fe, "password", service.ErrUserPasswordRequired)
		default:
			msgs[fe.Field()] = fe.Error() // fallback

		}
	}
	return msgs
}

func fieldMessage(fe validator.FieldError, label string, requiredErr error) string {
	switch fe.Tag() {
	case "required":
		return requiredErr.Error() // "user phone must be filled", dst.
	case "min":
		return fmt.Sprintf("%s must be at least %s characters", label, fe.Param())
	case "max":
		return fmt.Sprintf("%s must be at most %s characters", label, fe.Param())
	}
	return fe.Error()
}
