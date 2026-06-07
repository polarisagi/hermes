package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/polarisagi/hermes/internal/domain"
)

func (h *AdminHandler) HandleSmartRoutings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		routes, err := h.routeRepo.GetAllUserCustomRoutes(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if routes == nil {
			routes = []domain.UserCustomRoute{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(routes)

	case http.MethodPost:
		var rt domain.UserCustomRoute
		if err := json.NewDecoder(r.Body).Decode(&rt); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := h.routeRepo.CreateUserCustomRoute(r.Context(), &rt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		go h.reloadPipeline(context.Background())
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "id": rt.ID})

	case http.MethodPut:
		var rt domain.UserCustomRoute
		if err := json.NewDecoder(r.Body).Decode(&rt); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := h.routeRepo.UpdateUserCustomRoute(r.Context(), &rt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		go h.reloadPipeline(context.Background())
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "success"})

	case http.MethodDelete:
		idStr := r.URL.Query().Get("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := h.routeRepo.DeleteUserCustomRoute(r.Context(), id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		go h.reloadPipeline(context.Background())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success"}`))

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *AdminHandler) HandleIntents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		intents, err := h.intentRepo.GetAllUserIntents(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		type IntentItem struct {
			ModelID        string `json:"model_id"`
			CapabilityTier string `json:"capability_tier"`
		}
		var list []IntentItem
		for k, v := range intents {
			list = append(list, IntentItem{ModelID: k, CapabilityTier: v})
		}
		if list == nil {
			list = []IntentItem{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(list)

	case http.MethodPost:
		var payload struct {
			ModelID        string `json:"model_id"`
			CapabilityTier string `json:"capability_tier"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if payload.ModelID == "" || payload.CapabilityTier == "" {
			http.Error(w, "model_id and capability_tier are required", http.StatusBadRequest)
			return
		}
		validTiers := map[string]bool{"smart": true, "fast": true}
		if !validTiers[payload.CapabilityTier] {
			http.Error(w, "capability_tier must be one of: smart, fast", http.StatusBadRequest)
			return
		}
		if err := h.intentRepo.SaveUserIntent(r.Context(), &domain.UserModelIntentDict{
			ModelID:        payload.ModelID,
			CapabilityTier: payload.CapabilityTier,
			Source:         "manual",
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		go h.reloadPipeline(context.Background())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success"}`))

	case http.MethodDelete:
		modelID := r.URL.Query().Get("model")
		if modelID == "" {
			http.Error(w, "model query parameter is required", http.StatusBadRequest)
			return
		}
		if err := h.intentRepo.DeleteUserIntent(r.Context(), modelID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		go h.reloadPipeline(context.Background())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success"}`))

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
