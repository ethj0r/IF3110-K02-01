package Admin

import (
	Errors "errors"
	Strings "strings"
	Testing "testing"
)

func TestCreateUserInputValidation(T *Testing.T) {
	Valid := CreateUserInput{Username: " team.member ", Password: "correct horse battery staple"}
	Valid = Valid.Normalized()
	if Valid.Username != "team.member" {
		T.Fatalf("username was not trimmed: %q", Valid.Username)
	}
	if Err := Valid.Validate(); Err != nil {
		T.Fatalf("valid user input was rejected: %v", Err)
	}
	for _, Input := range []CreateUserInput{
		{Username: "ab", Password: "correct horse battery staple"},
		{Username: "bad name", Password: "correct horse battery staple"},
		{Username: "valid-name", Password: "short"},
		{Username: "valid-name", Password: Strings.Repeat("x", 1025)},
	} {
		if Err := Input.Validate(); !Errors.Is(Err, ErrInvalidInput) {
			T.Fatalf("invalid user input was accepted: %+v", Input)
		}
	}
}

func TestUpdateAndGroupInputValidation(T *Testing.T) {
	Active := true
	if Err := (UpdateUserInput{IsActive: &Active}).Validate(); Err != nil {
		T.Fatalf("valid activation was rejected: %v", Err)
	}
	if Err := (UpdateUserInput{}).Validate(); !Errors.Is(Err, ErrInvalidInput) {
		T.Fatal("empty user update was accepted")
	}
	ValidGroup := CreateGroupInput{Name: " Data Team ", Description: "Reads reporting data"}
	ValidGroup = ValidGroup.Normalized()
	if ValidGroup.Name != "Data Team" {
		T.Fatalf("group name was not trimmed: %q", ValidGroup.Name)
	}
	if Err := ValidGroup.Validate(); Err != nil {
		T.Fatalf("valid group was rejected: %v", Err)
	}
	for _, Input := range []CreateGroupInput{
		{Name: ""},
		{Name: "bad/name"},
		{Name: "valid", Description: Strings.Repeat("x", 501)},
	} {
		if Err := Input.Validate(); !Errors.Is(Err, ErrInvalidInput) {
			T.Fatalf("invalid group input was accepted: %+v", Input)
		}
	}
	Name := "Renamed"
	if Err := (UpdateGroupInput{Name: &Name}).Validate(); Err != nil {
		T.Fatalf("valid group update was rejected: %v", Err)
	}
	if Err := (UpdateGroupInput{}).Validate(); !Errors.Is(Err, ErrInvalidInput) {
		T.Fatal("empty group update was accepted")
	}
	if Err := (UpdateGroupInput{Name: StringPointer("bad/name")}).Validate(); !Errors.Is(Err, ErrInvalidInput) {
		T.Fatal("invalid group rename was accepted")
	}
	Empty := ""
	if Err := (UpdateGroupInput{Description: &Empty}).Validate(); Err != nil {
		T.Fatalf("empty description should be allowed: %v", Err)
	}
}

func StringPointer(Value string) *string {
	return &Value
}
