package handlers

import (
	"net/http"
)

type GetHealthzHandler struct {
	instanceID string
}

func NewGetHealthzHandler(instanceID string) *GetHealthzHandler {
	return &GetHealthzHandler{
		instanceID: instanceID,
	}
}

func (h *GetHealthzHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Instance-ID", h.instanceID)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}
