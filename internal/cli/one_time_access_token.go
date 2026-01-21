package cli

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"github.com/OrcaCD/orca-cd/internal/database"
	"github.com/OrcaCD/orca-cd/internal/models"
	"github.com/spf13/cobra"
)

var tokenExpiry time.Duration

var oneTimeAccessTokenCmd = &cobra.Command{
	Use:   "one-time-access-token <username or email>",
	Short: "Generate a one-time access token for account recovery",
	Long: `Generate a one-time access token that allows a user to log in without their password.
This is useful for account recovery when a user has lost access to their account.

The token can be used only once and expires after the specified duration (default: 1 hour).

Example:
  orca-cli one-time-access-token john@example.com
  orca-cli one-time-access-token johndoe --expiry 24h`,
	Args: cobra.ExactArgs(1),
	Run:  runOneTimeAccessToken,
}

func init() {
	oneTimeAccessTokenCmd.Flags().DurationVarP(&tokenExpiry, "expiry", "e", time.Hour, "Token expiry duration (e.g., 1h, 24h, 30m)")
}

func runOneTimeAccessToken(cmd *cobra.Command, args []string) {
	userIdentifier := args[0]

	// Connect to database
	database.Connect()

	// Find user by username or email
	var user models.User
	result := database.DB.Where("username = ? OR email = ?", userIdentifier, userIdentifier).First(&user)
	if result.Error != nil {
		fmt.Fprintf(os.Stderr, "Error: User not found with username or email: %s\n", userIdentifier)
		os.Exit(1)
	}

	// Generate a secure random token
	token, err := generateSecureToken(32)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating token: %v\n", err)
		os.Exit(1)
	}

	// Create the one-time access token
	otat := models.OneTimeAccessToken{
		Token:     token,
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(tokenExpiry),
	}

	if err := database.DB.Create(&otat).Error; err != nil {
		fmt.Fprintf(os.Stderr, "Error creating access token: %v\n", err)
		os.Exit(1)
	}

	// Output the token
	fmt.Println()
	fmt.Println("One-time access token generated successfully!")
	fmt.Println()
	fmt.Printf("  User:       %s (%s)\n", user.Username, user.Email)
	fmt.Printf("  Token:      %s\n", token)
	fmt.Printf("  Expires at: %s\n", otat.ExpiresAt.Format(time.RFC3339))
	fmt.Println()
	fmt.Println("Share this token with the user. It can only be used once.")
	fmt.Printf("Recovery URL: <your-hub-url>/auth/recover?token=%s\n", token)
	fmt.Println()
}

// generateSecureToken generates a cryptographically secure random token
func generateSecureToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
