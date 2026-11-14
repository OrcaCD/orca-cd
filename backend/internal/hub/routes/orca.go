package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func OrcaHandler(c *gin.Context) {
	c.JSON(http.StatusTeapot, gin.H{"message": "I'm not a teapot, I'm an orca 🐋"})
}
