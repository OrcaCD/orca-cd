package controllers

import (
	"github.com/OrcaCD/orca-cd/internal/database"
	"github.com/OrcaCD/orca-cd/internal/models"
	"net/http"

	"github.com/gin-gonic/gin"
)

type CreateMessageRequest struct {
	Text string `json:"text" binding:"required"`
}

func GetMessages(c *gin.Context) {
	var messages []models.Message

	if err := database.DB.Order("created_at desc").Find(&messages).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch messages"})
		return
	}

	c.JSON(http.StatusOK, messages)
}

func CreateMessage(c *gin.Context) {
	var req CreateMessageRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	message := models.Message{
		Text: req.Text,
	}

	if err := database.DB.Create(&message).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create message"})
		return
	}

	c.JSON(http.StatusCreated, message)
}
