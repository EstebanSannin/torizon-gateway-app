// Package auth handles authentication: first-boot admin setup, password hashing
// (argon2id), session management (secure HttpOnly SameSite cookies), CSRF, and
// login rate-limiting. Optional TOTP 2FA is designed-for but deferred past MVP.
//
// IMPLEMENTED (Phase 2): NeedsSetup, CreateAdmin (argon2id), Login (session),
// Validate, Logout — see auth.go / hash.go. Sessions + audit live in
// internal/store (SQLite). CSRF, session cookies, and route protection live in
// internal/httpserver (security.go). No default credentials, ever. §9.
//
// STILL ROADMAP: optional TOTP 2FA; login rate-limiting / lockout; RBAC.
package auth
