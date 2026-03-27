package router

import (
	"net/http"

	"github.com/suncrestlabs/nester/apps/api/internal/handler"
)

func Register(
	mux *http.ServeMux,
	authHandler *handler.AuthHandler,
	vaultHandler *handler.VaultHandler,
	settlementHandler *handler.SettlementHandler,
	authMiddleware func(http.Handler) http.Handler,
) {
	authHandler.Register(mux)
	vaultHandler.RegisterProtected(mux, authMiddleware)
	settlementHandler.RegisterProtected(mux, authMiddleware)
}
