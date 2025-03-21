package handlers

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	"wowarmory/internal/api"
	"wowarmory/internal/config"
	"wowarmory/internal/models"
	redisClient "wowarmory/internal/redis"
)

// CharacterHandler handles character-related HTTP requests
type CharacterHandler struct {
	config         *config.Config
	blizzardClient *api.BlizzardClient
	redisClient    *redisClient.Client
	templates      *template.Template
}

// GetTemplates returns the templates used by the handler
func (h *CharacterHandler) GetTemplates() *template.Template {
	return h.templates
}

// NewCharacterHandler creates a new CharacterHandler
func NewCharacterHandler(cfg *config.Config, blizzardClient *api.BlizzardClient, redisClient *redisClient.Client) (*CharacterHandler, error) {
	// Parse templates
	templatesPath := filepath.Join(cfg.TemplatesDir, "*.html")
	tmpl, err := template.ParseGlob(templatesPath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse templates: %w", err)
	}

	return &CharacterHandler{
		config:         cfg,
		blizzardClient: blizzardClient,
		redisClient:    redisClient,
		templates:      tmpl,
	}, nil
}

// LookupCharacter handles the character lookup request
func (h *CharacterHandler) LookupCharacter(w http.ResponseWriter, r *http.Request) {
	// Set content type
	w.Header().Set("Content-Type", "text/html")

	// Execute template
	if err := h.templates.ExecuteTemplate(w, "layout.html", nil); err != nil {
		http.Error(w, "Error executing template: "+err.Error(), http.StatusInternalServerError)
		return
	}
}

// GetCharacterTemplate handles the htmx request for the character template
func (h *CharacterHandler) GetCharacterTemplate(w http.ResponseWriter, r *http.Request) {
	// Get query parameters
	region := strings.ToLower(r.URL.Query().Get("region"))
	realm := strings.ToLower(r.URL.Query().Get("realm"))
	character := strings.ToLower(r.URL.Query().Get("character"))

	// Check if all required parameters are provided
	if region == "" || realm == "" || character == "" {
		http.Error(w, "Missing required parameters", http.StatusBadRequest)
		return
	}

	// Get access token
	accessToken, err := h.blizzardClient.GetAccessToken()
	if err != nil {
		http.Error(w, "Error getting access token: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Get character profile
	profileData, err := h.blizzardClient.GetCharacterProfile(accessToken, region, realm, character)
	if err != nil {
		h.templates.ExecuteTemplate(w, "error.html", map[string]string{
			"url": fmt.Sprintf("https://worldofwarcraft.blizzard.com/en-gb/character/%s/%s/%s", region, realm, character),
		})
		fmt.Printf("Error getting character profile: %v\n", err)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// Create character data from profile data
	data, err := models.NewCharacterData(profileData)
	if err != nil {
		http.Error(w, "Error processing character data: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Record the successful search in Redis
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := h.redisClient.RecordSearch(ctx, region, realm, character); err != nil {
		// Log the error but continue with the request
		fmt.Printf("Error recording search: %v\n", err)
	}

	// Set content type
	w.Header().Set("Content-Type", "text/html")

	// Execute only the character template
	if err := h.templates.ExecuteTemplate(w, "character", data); err != nil {
		http.Error(w, "Error executing template: "+err.Error(), http.StatusInternalServerError)
		return
	}
}
