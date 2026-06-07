package api

import (
	"encoding/json"
	"net/http"
)

func (h *AdminHandler) GetClientsStatus(w http.ResponseWriter, r *http.Request) {
	statuses, err := h.clientSvc.GetAllStatuses(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"clients": statuses})
}

func (h *AdminHandler) ApplyClientConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload struct {
		Client string `json:"client"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.Client == "" {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := h.clientSvc.ApplyConfig(r.Context(), payload.Client); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"success"}`))
}

func (h *AdminHandler) RestoreClientConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload struct {
		Client string `json:"client"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.Client == "" {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := h.clientSvc.RestoreConfig(r.Context(), payload.Client); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"success"}`))
}
