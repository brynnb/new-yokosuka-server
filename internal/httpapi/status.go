package httpapi

import "net/http"

type StatusHandler struct{}

func NewStatusHandler() *StatusHandler {
	return &StatusHandler{}
}

func (h *StatusHandler) ServeHTTP(
	response http.ResponseWriter,
	request *http.Request,
) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		writeError(response, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(response, http.StatusOK, map[string]bool{
		"online": true,
	})
}
