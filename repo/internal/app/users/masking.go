package users

// SafeUser is the non-admin view of a user: personal data is masked.
type SafeUser struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"` // masked if non-admin
	IsActive    bool   `json:"is_active"`
}

// Mask applies field-level masking for non-admin callers.
// isAdmin controls whether sensitive fields are revealed.
func Mask(u *UserWithRoles, isAdmin bool) SafeUser {
	email := u.Email
	if !isAdmin {
		email = maskEmail(email)
	}
	return SafeUser{
		ID:          u.ID,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		Email:       email,
		IsActive:    u.IsActive,
	}
}

// maskEmail replaces everything before @ with asterisks, keeping first character.
func maskEmail(email string) string {
	at := 0
	for i, c := range email {
		if c == '@' {
			at = i
			break
		}
	}
	if at <= 1 {
		return "***@" + email[at+1:]
	}
	return string(email[0]) + "***@" + email[at+1:]
}
