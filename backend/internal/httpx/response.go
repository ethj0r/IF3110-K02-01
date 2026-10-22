package Httpx

import (
	Json "encoding/json"
	Http "net/http"
)

type ErrorResponse struct {
	Error APIError `json:"error"`
}

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func JSON(W Http.ResponseWriter, Status int, Value any) {
	Body, Err := Json.Marshal(Value)
	if Err != nil {
		Error(W, Http.StatusInternalServerError, "internal_error", "An unexpected error occurred")
		return
	}
	W.Header().Set("Content-Type", "application/json; charset=utf-8")
	W.Header().Set("X-Content-Type-Options", "nosniff")
	W.WriteHeader(Status)
	_, _ = W.Write(append(Body, '\n'))
}

func Error(W Http.ResponseWriter, Status int, Code, Message string) {
	JSON(W, Status, ErrorResponse{Error: APIError{Code: Code, Message: Message}})
}
