package integrations

import (
	"errors"
	"fmt"
	"net/url"
)

// Code is the detail code stored with an error and shown next to its generic message.
type Code string

const (
	CodeInvalid           Code = "invalid_input"
	CodeInvalidJSON       Code = "invalid_json"
	CodeNotServiceAccount Code = "not_service_account"
	CodeIncompleteKey     Code = "incomplete_key"
	CodeTooLarge          Code = "too_large"
	CodeNoStorage         Code = "no_storage"
	CodeNotConfigured     Code = "not_configured"
	CodeNoTarget          Code = "no_target"
	CodeTargetMissing     Code = "target_missing"
	CodeKeyChanged        Code = "key_changed"
	CodeAuth              Code = "auth_failed"
	CodeForbidden         Code = "forbidden"
	CodeNotFound          Code = "not_found"
	CodeRateLimited       Code = "rate_limited"
	CodeProviderDown      Code = "provider_down"
	CodeBadResponse       Code = "bad_response"
	CodeNetwork           Code = "network"
	CodeBusy              Code = "busy"
	CodeVaultManaged      Code = "vault_managed"
	CodeKeyMismatch       Code = "key_file_mismatch"
	CodeInternal          Code = "internal"
)

// Message is the generic, user-facing text of a code. Provider detail stays in the logs.
func (c Code) Message() string {
	switch c {
	case CodeInvalid:
		return "That value is not in the expected format."
	case CodeInvalidJSON:
		return "The file is not valid JSON."
	case CodeNotServiceAccount:
		return "This is not a service account key: its type must be service_account."
	case CodeIncompleteKey:
		return "The key lacks client_email or private_key."
	case CodeTooLarge:
		return "The credential is larger than 64 KB."
	case CodeNoStorage:
		return "Credential storage is off: SESSION_KEY is not set."
	case CodeNotConfigured:
		return "This integration is not configured."
	case CodeNoTarget:
		return "Pick a property, site or zone first."
	case CodeTargetMissing:
		return "The account cannot see the selected property or site."
	case CodeKeyChanged:
		return "The credential was saved under a different SESSION_KEY. Enter it again."
	case CodeAuth:
		return "The provider rejected the credential."
	case CodeForbidden:
		return "The credential lacks permission for this."
	case CodeNotFound:
		return "The provider does not know this property, site or zone."
	case CodeRateLimited:
		return "The provider is rate limiting requests. Try again later."
	case CodeProviderDown:
		return "The provider had an error. Try again later."
	case CodeBadResponse:
		return "The provider answered unexpectedly."
	case CodeNetwork:
		return "The provider could not be reached."
	case CodeBusy:
		return "A sync is already running."
	case CodeVaultManaged:
		return "This credential is managed by Vault and cannot be changed here."
	case CodeKeyMismatch:
		return "The key file is not served at its key location."
	case CodeInternal:
		return "Something went wrong."
	default:
		return "Something went wrong."
	}
}

// CodedError is an error with a stored code; Err is the detail for the logs only.
type CodedError struct {
	Code   Code
	Status int
	Err    error
}

func (f *CodedError) Error() string {
	if f.Err != nil {
		return fmt.Sprintf("%s: %v", f.Code, f.Err)
	}
	return string(f.Code)
}

func (f *CodedError) Unwrap() error { return f.Err }

// Fail is a CodedError without detail.
func Fail(c Code) error { return &CodedError{Code: c} }

// CodeOf extracts the code of any error; unknown errors are internal, nil has none.
func CodeOf(err error) Code {
	if err == nil {
		return ""
	}
	var f *CodedError
	if errors.As(err, &f) {
		return f.Code
	}
	if errors.Is(err, ErrKeyChanged) {
		return CodeKeyChanged
	}
	return CodeInternal
}

// statusCode maps a provider's HTTP status to a code.
func statusCode(status int) Code {
	switch {
	case status == 401:
		return CodeAuth
	case status == 403:
		return CodeForbidden
	case status == 404:
		return CodeNotFound
	case status == 429:
		return CodeRateLimited
	case status >= 500:
		return CodeProviderDown
	default:
		return CodeBadResponse
	}
}

// redact drops the request URL from a transport error: Bing puts its API key in the query string.
func redact(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return &CodedError{Code: CodeNetwork, Err: fmt.Errorf("%s: %w", ue.Op, ue.Err)}
	}
	return &CodedError{Code: CodeNetwork, Err: err}
}
