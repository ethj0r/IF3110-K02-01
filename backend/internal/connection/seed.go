package Connection

import (
	Context "context"
	Errors "errors"
	Net "net"
	Neturl "net/url"
	Strconv "strconv"
	Strings "strings"
)

func ParseDatabaseURL(Raw string) (ConnectionInput, error) {
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
	Input := ConnectionInput{Name: "local-target", Host: Host, Port: Port, DBName: DBName, Username: Username, Password: Password}
	if Err := Input.Validate(); Err != nil {
		return ConnectionInput{}, Err
	}
	return Input, nil
}

func SeedDefault(Ctx Context.Context, Server *Service, Raw string) error {
	if Strings.TrimSpace(Raw) == "" {
		return nil
	}
	Existing, Err := Server.List(Ctx)
	if Err != nil {
		return Err
	}
	if len(Existing) > 0 {
		return nil
	}
	Input, Err := ParseDatabaseURL(Raw)
	if Err != nil {
		return Err
	}
	_, Err = Server.Create(Ctx, Input, nil)
	if Errors.Is(Err, ErrConflict) {
		return nil
	}
	return Err
}
