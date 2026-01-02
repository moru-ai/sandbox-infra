package handlers

import (
	"log"
	"net/http"
	"strings"

	"github.com/moru-ai/sandbox-infra/packages/docker-reverse-proxy/internal/utils"
)

// LoginWithToken Validates the token by checking if the generated token is in the cache.
func (a *APIStore) LoginWithToken(w http.ResponseWriter, r *http.Request) error {
	authHeader := r.Header.Get("Authorization")
	moruToken := strings.TrimPrefix(authHeader, "Bearer ")
	_, err := a.AuthCache.Get(moruToken)
	if err != nil {
		log.Printf("Error while logging with access token: %s, header: %s\n", err, authHeader)
		utils.SetDockerUnauthorizedHeaders(w)

		return err
	}

	return nil
}
