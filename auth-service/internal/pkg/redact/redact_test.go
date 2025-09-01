package redact

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEmail_LocalLong_PrefixTwoPlusStars(t *testing.T) {
	got := Email("foobar@example.com")
	require.Equal(t, "fo***@example.com", got)
}

func TestEmail_LocalShort_OneChar(t *testing.T) {
	got := Email("a@ex.com")
	require.Equal(t, "***@ex.com", got)
}

func TestEmail_LocalShort_TwoChars(t *testing.T) {
	got := Email("ab@ex.com")
	require.Equal(t, "***@ex.com", got)
}

func TestEmail_Invalid_NoAt_ReturnsStars(t *testing.T) {
	got := Email("no-at-here")
	require.Equal(t, "***", got)
}

func TestEmail_Invalid_MultipleAt_ReturnsStars(t *testing.T) {
	got := Email("a@b@c")
	require.Equal(t, "***", got)
}

func TestEmail_PreservesDomain_CaseAndContent(t *testing.T) {
	got := Email("abc.def+tag@EXAMPLE.org")
	require.Equal(t, "ab***@EXAMPLE.org", got)
}

func TestEmail_EmptyString_ReturnsStars(t *testing.T) {
	require.Equal(t, "***", Email(""))
}

func TestEmail_EmptyDomainAllowedByImpl(t *testing.T) {
	got := Email("user@")
	require.Equal(t, "us***@", got)
}

func TestToken_And_Password_Literals(t *testing.T) {
	require.Equal(t, "[REDACTED_TOKEN]", Token())
	require.Equal(t, "[REDACTED_PASSWORD]", Password())
}
