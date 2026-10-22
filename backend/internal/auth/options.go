package Auth

import (
	Base64 "encoding/base64"
	Errors "errors"
	Url "net/url"
	Os "os"
	Strings "strings"
	Time "time"
)

type Options struct {
	Origin           string
	CookieSecure     bool
	EncryptionKey    []byte
	IdleTimeout      Time.Duration
	AbsoluteTimeout  Time.Duration
	RotationInterval Time.Duration
	GracePeriod      Time.Duration
}

const CookieName = "platform_session"

func DefaultOptions() Options {
	return Options{Origin: "http://127.0.0.1:5173", IdleTimeout: 30 * Time.Minute, AbsoluteTimeout: 12 * Time.Hour, RotationInterval: 15 * Time.Minute, GracePeriod: 30 * Time.Second}
}

func LoadOptions() (Options, error) {
	Result := DefaultOptions()
	Environment := Os.Getenv("APP_ENV")
	if Environment != "" && Environment != "development" && Environment != "production" {
		return Options{}, Errors.New("APP_ENV must be development or production")
	}
	if Origin, Ok := Os.LookupEnv("APP_ORIGIN"); Ok {
		Result.Origin = Origin
	}
	Parsed, Err := Url.Parse(Result.Origin)
	if Err != nil || Parsed.Hostname() == "" || (Parsed.Scheme != "http" && Parsed.Scheme != "https") || Parsed.User != nil || (Parsed.Path != "" && Parsed.Path != "/") || Parsed.RawQuery != "" || Parsed.Fragment != "" {
		return Options{}, Errors.New("APP_ORIGIN must be an HTTP or HTTPS origin")
	}
	Result.Origin = Parsed.Scheme + "://" + Strings.ToLower(Parsed.Host)
	Result.CookieSecure = Parsed.Scheme == "https"
	if Environment == "production" && !Result.CookieSecure {
		return Options{}, Errors.New("production requires an HTTPS APP_ORIGIN")
	}
	Result.EncryptionKey, Err = Base64.StdEncoding.Strict().DecodeString(Os.Getenv("SESSION_KEY"))
	if Err != nil || len(Result.EncryptionKey) != 32 {
		return Options{}, Errors.New("SESSION_KEY must be a base64 encoded 32-byte key")
	}
	for Name, Destination := range map[string]*Time.Duration{
		"SESSION_IDLE_TIMEOUT":      &Result.IdleTimeout,
		"SESSION_ABSOLUTE_TIMEOUT":  &Result.AbsoluteTimeout,
		"SESSION_ROTATION_INTERVAL": &Result.RotationInterval,
		"SESSION_GRACE_PERIOD":      &Result.GracePeriod,
	} {
		if Value, Ok := Os.LookupEnv(Name); Ok {
			Parsed, Err := Time.ParseDuration(Value)
			if Err != nil {
				return Options{}, Errors.New(Name + " must be a duration")
			}
			*Destination = Parsed
		}
	}
	if Err := Result.Validate(); Err != nil {
		return Options{}, Err
	}
	return Result, nil
}

func (Settings Options) Validate() error {
	Parsed, Err := Url.Parse(Settings.Origin)
	if Err != nil || Parsed.Host == "" || (Parsed.Scheme != "http" && Parsed.Scheme != "https") || Parsed.User != nil || Parsed.Path != "" || Parsed.RawQuery != "" || Parsed.Fragment != "" || (Parsed.Scheme == "https" && !Settings.CookieSecure) {
		return Errors.New("invalid authentication origin or cookie security")
	}
	if len(Settings.EncryptionKey) != 32 {
		return Errors.New("SESSION_KEY must contain 32 bytes")
	}
	if Settings.IdleTimeout <= 0 || Settings.AbsoluteTimeout <= 0 || Settings.RotationInterval <= 0 || Settings.GracePeriod <= 0 || Settings.IdleTimeout > Settings.AbsoluteTimeout || Settings.RotationInterval >= Settings.AbsoluteTimeout || Settings.GracePeriod >= Settings.RotationInterval || Settings.GracePeriod >= Settings.IdleTimeout {
		return Errors.New("invalid session timeout or rotation configuration")
	}
	return nil
}
