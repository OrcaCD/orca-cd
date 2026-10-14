package routes

import (
	"strings"

	"github.com/gin-gonic/gin"
)

var (
	likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	// Enum values such as "commit_sync" or "rotated-token" are matched with their
	// separators replaced by spaces, so searching for the label shown in the UI works.
	enumSeparatorReplacer = strings.NewReplacer("_", " ", "-", " ")
)

type searchPatterns struct {
	// Text matches the search term anywhere in a free text column.
	Text string
	// Enum matches the search term against an enum column whose separators were
	// replaced by spaces (see enumSearchColumn).
	Enum string
}

// searchQuery reads the "search" query parameter and turns it into LIKE patterns.
// Wildcards in the term are escaped, so the patterns must be used with ESCAPE '\'.
func searchQuery(c *gin.Context) (searchPatterns, bool) {
	term := strings.TrimSpace(c.Query("search"))
	if term == "" {
		return searchPatterns{}, false
	}
	return searchPatterns{
		Text: "%" + likeEscaper.Replace(term) + "%",
		Enum: "%" + likeEscaper.Replace(enumSeparatorReplacer.Replace(term)) + "%",
	}, true
}

// enumSearchColumn returns a SQL expression for column with "_" and "-" replaced by spaces.
func enumSearchColumn(column string) string {
	return "REPLACE(REPLACE(" + column + ", '_', ' '), '-', ' ')"
}
