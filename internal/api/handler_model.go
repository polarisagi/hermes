package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/polarisagi/hermes/internal/domain"
	"github.com/polarisagi/hermes/internal/router"
	modelsync "github.com/polarisagi/hermes/internal/sync"
)

func (h *AdminHandler) HandleModels(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		models, err := h.modelRepo.GetUserModels(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(models)

	case http.MethodPost:
		var m domain.UserModel
		if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if m.UserProviderID <= 0 || m.ModelID == "" {
			http.Error(w, "user_provider_id and model_id are required", http.StatusBadRequest)
			return
		}
		if m.CapabilityTier == "" {
			m.CapabilityTier = "smart"
		}
		if err := h.modelRepo.CreateUserModel(r.Context(), &m); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		go h.reloadAll(context.Background())
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "id": m.ID})

	case http.MethodPut:
		var payload struct {
			ID             int    `json:"id"`
			CapabilityTier string `json:"capability_tier"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if payload.ID <= 0 || payload.CapabilityTier == "" {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		if err := h.modelRepo.UpdateUserModelTier(r.Context(), payload.ID, payload.CapabilityTier); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		go h.reloadAll(context.Background())
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "msg": "Model capability tier updated"})

	case http.MethodDelete:
		idStr := r.URL.Query().Get("id")
		id, err := strconv.Atoi(idStr)
		if err != nil || id <= 0 {
			http.Error(w, "invalid id", http.StatusBadRequest)
			return
		}
		if err := h.modelRepo.DeleteUserModel(r.Context(), id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		go h.reloadAll(context.Background())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success"}`))

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *AdminHandler) SyncModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()

	providers, err := h.providerRepo.GetUserProviders(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	inferer := router.NewIntentInferer(h.intentRepo)
	var totalSynced int

	for _, p := range providers {
		if p.Status != 1 {
			continue
		}

		sysEps, err := h.providerRepo.GetSysAccessEndpointsByProvider(ctx, p.ProviderID)
		if err != nil || len(sysEps) == 0 {
			continue
		}

		var matchedEp *domain.SysAccessEndpoint
		var baseURL string
		for _, sysEp := range sysEps {
			if sysEp.APIProtocol == "openai" || sysEp.APIProtocol == "anthropic" || sysEp.APIProtocol == "google" {
				if sysEp.AuthType == "adc" {
					continue
				}
				baseURL = strings.TrimSuffix(sysEp.DefaultBaseURL, "/")
				for _, userEp := range p.Endpoints {
					if userEp.SysEndpointID == sysEp.EndpointID && userEp.CustomBaseURL != "" {
						baseURL = strings.TrimSuffix(userEp.CustomBaseURL, "/")
					}
				}
				epCopy := sysEp
				matchedEp = &epCopy
				break
			}
		}

		if matchedEp == nil || baseURL == "" {
			continue
		}

		var creds map[string]string
		if err := json.Unmarshal(p.AuthCredentials, &creds); err != nil || creds["api_key"] == "" {
			continue
		}
		apiKey := creds["api_key"]

		client := &http.Client{Timeout: 10 * time.Second}
		var req *http.Request
		var errReq error

		switch matchedEp.APIProtocol {
		case "openai":
			req, errReq = http.NewRequestWithContext(ctx, "GET", baseURL+"/v1/models", nil)
			if errReq == nil {
				req.Header.Set("Authorization", "Bearer "+apiKey)
			}
		case "anthropic":
			req, errReq = http.NewRequestWithContext(ctx, "GET", baseURL+"/models", nil)
			if errReq == nil {
				req.Header.Set("x-api-key", apiKey)
				req.Header.Set("anthropic-version", "2023-06-01")
			}
		case "google":
			req, errReq = http.NewRequestWithContext(ctx, "GET", baseURL+"/models?key="+apiKey, nil)
		}

		if errReq != nil || req == nil {
			continue
		}

		resp, err := client.Do(req)
		if err != nil || resp.StatusCode != 200 {
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}

		var res struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
			Models []struct {
				Name string `json:"name"`
			} `json:"models"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
			resp.Body.Close()
			continue
		}
		resp.Body.Close()

		var modelIDs []string
		if len(res.Data) > 0 {
			for _, m := range res.Data {
				modelIDs = append(modelIDs, m.ID)
			}
		} else if len(res.Models) > 0 {
			for _, m := range res.Models {
				id := strings.TrimPrefix(m.Name, "models/")
				modelIDs = append(modelIDs, id)
			}
		}

		for _, modelID := range modelIDs {
			weight := inferer.ParseVersionWeight(modelID)
			tier, source := inferer.InferTierOnly(ctx, modelID)
			isLegacy := inferer.IsLegacyModel(modelID)

			_ = h.modelRepo.UpsertSysModel(ctx, &domain.SysModel{
				ModelID:        modelID,
				DisplayName:    modelID,
				VersionWeight:  weight,
				IsLegacy:       isLegacy,
				CapabilityTier: tier,
			})
			_ = h.modelRepo.UpsertSysProviderModel(ctx, &domain.SysProviderModel{
				ProviderID:    p.ProviderID,
				ModelID:       modelID,
				ActualModelID: modelID,
			})
			_ = h.intentRepo.SaveSysIntent(ctx, &domain.UserModelIntentDict{
				ModelID:        modelID,
				CapabilityTier: tier,
				Source:         source,
			})
			totalSynced++
		}
	}

	go h.reloadAll(context.Background())

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "synced": totalSynced})
}

func (h *AdminHandler) SyncGlobalModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	inferer := router.NewIntentInferer(h.intentRepo)
	syncSvc := modelsync.NewSyncService(h.extCacheRepo, inferer)
	promoteSvc := modelsync.NewPromoteService(h.extCacheRepo, h.modelRepo, h.intentRepo, h.providerRepo)

	go func() {
		modelsync.ScheduledSync(context.Background(), syncSvc, promoteSvc)
	}()

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"success","message":"Global model sync started in background"}`))
}
