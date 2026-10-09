package locale

func init() {
	Register("language must be en", "language must be en")
	Register("task not found", "task not found")
	Register("no active task", "no active task")
}

func init() {
	Register("Unauthorized", "Unauthorized")
	Register("Invalid or expired token", "Invalid or expired token")
	Register("Password is already set", "Password is already set")
	Register("Password cannot be empty", "Password cannot be empty")
	Register("Password hashing failed", "Password hashing failed")
	Register("Save failed: ", "Save failed: ")
	Register("Token generation failed", "Token generation failed")
	Register("Invalid request format", "Invalid request format")
	Register("New password cannot be empty", "New password cannot be empty")
	Register("Password is not initialized; set it first", "Password is not initialized; set it first")
	Register("Current password is incorrect", "Current password is incorrect")
	Register("Incorrect username or password", "Incorrect username or password")
	Register("[auth] JWT key migrated from %s to %s (outside the browsable workspace)", "[auth] JWT key migrated from %s to %s (outside the browsable workspace)")
	Register("[auth] New JWT key written to %s", "[auth] New JWT key written to %s")
}

func init() {
	Register("Preparing…", "Preparing…")
	Register("Update failed", "Update failed")
	Register("New version ready; restarting…", "New version ready; restarting…")
	Register("Current version %q is not a stable release; one-click updates are disabled", "Current version %q is not a stable release; one-click updates are disabled")
	Register("Already running the latest version %s", "Already running the latest version %s")
	Register("An update is already in progress", "An update is already in progress")
	Register("[update] Update failed: %v", "[update] Update failed: %v")
	Register("[update] %s -> %s staged; exiting to complete replacement", "[update] %s -> %s staged; exiting to complete replacement")
	Register("Cannot roll back while an update is in progress", "Cannot roll back while an update is in progress")
	Register("[update] Manually rolled back to the previous version; exiting to complete the switch", "[update] Manually rolled back to the previous version; exiting to complete the switch")
}
