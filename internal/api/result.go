package api

// Result is the transport envelope shared by the desktop services.
type Result struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error *Error `json:"error,omitempty"`
}

type Error struct {
	Code    string           `json:"code"`
	Message string           `json:"message"`
	Context map[string]int64 `json:"context"`
}

func Success(data any) Result { return Result{OK: true, Data: data} }
func Failure(code, message string) Result {
	return Result{Error: &Error{Code: code, Message: message, Context: map[string]int64{}}}
}
