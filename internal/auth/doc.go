// Package auth handles authentication: first-boot admin setup, password hashing
// (argon2id), session management (secure HttpOnly SameSite cookies), CSRF, and
// login rate-limiting. Optional TOTP 2FA is designed-for but deferred past MVP.
//
// ROADMAP (Phase 0 first-boot + login). Planned surface:
//
//	type Service interface {
//	    NeedsSetup(ctx) bool                      // no admin yet → force /setup
//	    CreateAdmin(ctx, user, password) error    // argon2id hash → store
//	    Login(ctx, user, password) (Session, error)
//	    Validate(ctx, sessionID) (Session, bool)
//	    Logout(ctx, sessionID) error
//	}
//
// State lives in internal/store (SQLite). No default credentials, ever. §9.
package auth
