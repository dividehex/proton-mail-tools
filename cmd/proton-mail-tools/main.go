// Command proton-mail-tools serves email tools for OpenWebUI backed by Proton Mail Bridge.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"proton-mail-tools/internal/bridge"
	"proton-mail-tools/internal/config"
	"proton-mail-tools/internal/httpapi"
	"proton-mail-tools/internal/mail"
	"proton-mail-tools/internal/service"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(log)

	cfg, err := config.FromEnv()
	if err != nil {
		log.Error("configuration", "err", err)
		os.Exit(1)
	}
	if cfg.APIKey == "" {
		log.Warn("API_KEY is empty: tool endpoints are unauthenticated")
	}

	imapTLS := bridge.TLSConfig(cfg.IMAPAddr, cfg.TLSSkipVerify)
	smtpTLS := bridge.TLSConfig(cfg.SMTPAddr, cfg.TLSSkipVerify)
	store := bridge.NewIMAPStore(cfg.IMAPAddr, cfg.Username, cfg.Password, imapTLS)
	sender := bridge.NewSMTPSender(cfg.SMTPAddr, cfg.Username, cfg.Password, smtpTLS)

	svc := service.New(store, sender, service.Options{
		From:         mail.Address{Name: cfg.FromName, Email: cfg.FromAddress},
		AllowSend:    cfg.AllowSend,
		AllowDelete:  cfg.AllowDelete,
		AllowPurge:   cfg.AllowPurge,
		MaxBodyChars: cfg.MaxBodyChars,
		DefaultLimit: cfg.DefaultSearchLimit,
		MaxLimit:     cfg.MaxSearchLimit,
	}, log)

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           httpapi.New(svc, cfg.APIKey),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Info("listening", "addr", cfg.ListenAddr, "imap", cfg.IMAPAddr, "smtp", cfg.SMTPAddr,
			"allow_send", cfg.AllowSend, "allow_delete", cfg.AllowDelete, "allow_purge", cfg.AllowPurge)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown", "err", err)
	}
	log.Info("stopped")
}
