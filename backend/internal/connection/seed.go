package Connection

import (
	Context "context"
	Errors "errors"
	Net "net"
	Neturl "net/url"
	Strconv "strconv"
	Strings "strings"
	Utf8 "unicode/utf8"
)

type SeedTarget struct {
	Name string
	URL  string
}

func DerivedName(DBName string) string {
	Name := "local-" + Strings.TrimSpace(DBName)
	for len(Name) > 64 {
		_, Size := Utf8.DecodeLastRuneInString(Name)
		if Size <= 0 {
			break
		}
		Name = Name[:len(Name)-Size]
	}
	return Name
}

func ParseDatabaseURL(Raw string) (ConnectionInput, error) {
	return ParseDatabaseURLNamed(Raw, "local-target")
}

func ParseDatabaseURLNamed(Raw, Name string) (ConnectionInput, error) {
	Parsed, Err := Neturl.Parse(Raw)
	if Err != nil {
		return ConnectionInput{}, ErrInvalidInput
	}
	if Parsed.Scheme != "postgres" && Parsed.Scheme != "postgresql" {
		return ConnectionInput{}, ErrInvalidInput
	}
	Host := Parsed.Hostname()
	DBName := Strings.Trim(Parsed.Path, "/")
	Username := Parsed.User.Username()
	Password, _ := Parsed.User.Password()
	Port := 5432
	if Value := Parsed.Port(); Value != "" {
		PortNumber, Err := Strconv.Atoi(Value)
		if Err != nil || PortNumber < 1 || PortNumber > 65535 {
			return ConnectionInput{}, ErrInvalidInput
		}
		Port = PortNumber
	}
	if _, _, Err := Net.SplitHostPort(Net.JoinHostPort(Host, "5432")); Err != nil {
		return ConnectionInput{}, ErrInvalidInput
	}
	Input := ConnectionInput{Name: Strings.TrimSpace(Name), Host: Host, Port: Port, DBName: DBName, Username: Username, Password: Password}
	if Input.Name == "" {
		Input.Name = DerivedName(Input.DBName)
	}
	if Err := Input.Validate(); Err != nil {
		return ConnectionInput{}, Err
	}
	return Input, nil
}

func SeedTargetsEnabled(AppEnv, Override string) bool {
	if Strings.EqualFold(Strings.TrimSpace(Override), "true") {
		return true
	}
	return !Strings.EqualFold(Strings.TrimSpace(AppEnv), "production")
}

func SeedTargets(Ctx Context.Context, Server *Service, Targets ...SeedTarget) error {
	if len(Targets) == 0 {
		return nil
	}
	Existing, Err := Server.List(Ctx)
	if Err != nil {
		return Err
	}
	Seen := make(map[string]struct{}, len(Existing))
	for _, Item := range Existing {
		Seen[Strings.ToLower(Item.Name)] = struct{}{}
	}
	var Failures []error
	for _, Target := range Targets {
		if Strings.TrimSpace(Target.URL) == "" {
			continue
		}
		Input, Err := ParseDatabaseURLNamed(Target.URL, Target.Name)
		if Err != nil {
			Failures = append(Failures, Err)
			continue
		}
		Key := Strings.ToLower(Input.Name)
		if _, Ok := Seen[Key]; Ok {
			continue
		}
		_, Err = Server.Create(Ctx, Input, nil)
		if Errors.Is(Err, ErrConflict) {
			Seen[Key] = struct{}{}
			continue
		}
		if Err != nil {
			Failures = append(Failures, Err)
			continue
		}
		Seen[Key] = struct{}{}
	}
	return Errors.Join(Failures...)
}

func SeedDefault(Ctx Context.Context, Server *Service, Raw string) error {
	if Strings.TrimSpace(Raw) == "" {
		return nil
	}
	return SeedTargets(Ctx, Server, SeedTarget{Name: "local-target", URL: Raw})
}
