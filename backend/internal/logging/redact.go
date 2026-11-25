package Logging

import (
	Fmt "fmt"
	Slog "log/slog"
	Regexp "regexp"
	Strings "strings"
	Unicode "unicode/utf8"
)

const Redacted = "[REDACTED]"

var sensitiveKeys = []string{
	"password", "passwd", "secret", "token", "cookie", "csrf", "session_id", "sessionid",
	"identifier", "authorization", "dsn", "database_url", "ciphertext", "api_key", "apikey",
}

var (
	urlCredentials   = Regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://[^:/@\s]+):[^@\s]*@`)
	keywordSecrets   = Regexp.MustCompile(`(?i)\b(password|passwd|pwd|secret|token|sslpassword)\s*=\s*('[^']*'|"[^"]*"|\S+)`)
	sqlPassword      = Regexp.MustCompile(`(?i)\b(password)\s+('(?:[^']|'')*'|\$\$.*?\$\$)`)
	sensitiveSQLText = Regexp.MustCompile(`(?i)password|secret|token|cipher|session|csrf|identifier|\bhash\b|_hash\b`)
)

// SensitiveKey reports whether an attribute key names a credential or session secret.
func SensitiveKey(Key string) bool {
	Lower := Strings.ToLower(Key)
	for _, Fragment := range sensitiveKeys {
		if Strings.Contains(Lower, Fragment) {
			return true
		}
	}
	return false
}

// SanitizeLogValue strips CR and LF characters from log fields to prevent log
// forging / log injection (CWE-117).
func SanitizeLogValue(Value string) string {
	Value = Strings.ReplaceAll(Value, "\r", "")
	return Strings.ReplaceAll(Value, "\n", "")
}

// RedactString masks credentials embedded in connection URLs and key=value strings.
func RedactString(Value string) string {
	Value = urlCredentials.ReplaceAllString(Value, "$1:"+Redacted+"@")
	Value = keywordSecrets.ReplaceAllString(Value, "$1="+Redacted)
	return sqlPassword.ReplaceAllString(Value, "$1 '"+Redacted+"'")
}

// RedactAttr is a slog ReplaceAttr hook applied to every attribute, including nested groups.
func RedactAttr(_ []string, Attr Slog.Attr) Slog.Attr {
	if SensitiveKey(Attr.Key) {
		return Slog.String(Attr.Key, Redacted)
	}
	switch Attr.Value.Kind() {
	case Slog.KindString:
		return Slog.String(Attr.Key, RedactString(Attr.Value.String()))
	case Slog.KindAny:
		if Err, Ok := Attr.Value.Any().(error); Ok {
			return Slog.String(Attr.Key, RedactString(Err.Error()))
		}
	}
	return Attr
}

// Truncate shortens long values so huge SQL scripts or parameters cannot flood the log.
func Truncate(Value string, Limit int) string {
	if len(Value) <= Limit {
		return Value
	}
	Cut := Limit
	for Cut > 0 && !Unicode.RuneStart(Value[Cut]) {
		Cut--
	}
	return Fmt.Sprintf("%s…(%d bytes)", Value[:Cut], len(Value))
}

// SafeParams formats bound SQL parameters for logs. Every parameter is masked when
// the statement touches credentials or sessions; binary values are summarised.
func SafeParams(SQL string, Args []any) []string {
	if len(Args) == 0 {
		return nil
	}
	Result := make([]string, len(Args))
	Masked := sensitiveSQLText.MatchString(SQL)
	for Index, Arg := range Args {
		switch {
		case Masked:
			Result[Index] = Redacted
		case Arg == nil:
			Result[Index] = "NULL"
		default:
			switch Value := Arg.(type) {
			case []byte:
				Result[Index] = Fmt.Sprintf("[%d bytes]", len(Value))
			case string:
				Result[Index] = Truncate(RedactString(Value), 256)
			default:
				Result[Index] = Truncate(RedactString(Fmt.Sprint(Value)), 256)
			}
		}
	}
	return Result
}
