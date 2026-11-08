package routes

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/OrcaCD/orca-cd/internal/hub/utils"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"gorm.io/gorm"
)

type adminUserResponse struct {
	Id                     string             `json:"id"`
	Name                   string             `json:"name"`
	Email                  string             `json:"email"`
	Roles                  []userRoleResponse `json:"roles"`
	Providers              []string           `json:"providers"`
	PasswordChangeRequired bool               `json:"passwordChangeRequired"`
	CreatedAt              string             `json:"createdAt"`
	UpdatedAt              string             `json:"updatedAt"`
}

type adminUserWithGeneratedPasswordResponse struct {
	adminUserResponse
	GeneratedPassword string `json:"generatedPassword,omitempty"`
}

type adminCreateUserRequest struct {
	Name  string `json:"name" binding:"required,min=3,max=64"`
	Email string `json:"email" binding:"required,email"`
	// Without roleIds, the user gets the default role
	RoleIds []string `json:"roleIds" binding:"max=100"`
}

type adminUpdateUserRequest struct {
	// Name and email are validated by adminUserProfile, unless the profile is
	// managed by an OIDC provider.
	Name          string   `json:"name"`
	Email         string   `json:"email"`
	RoleIds       []string `json:"roleIds" binding:"max=100"`
	ResetPassword bool     `json:"resetPassword"`
}

type adminUserProfile struct {
	Name  string `binding:"required,min=3,max=64"`
	Email string `binding:"required,email"`
}

// userRoleResponse is a role of a user. Sources tells whether an admin assigned
// it, it comes from an OIDC group, or both.
type userRoleResponse struct {
	Id      string                  `json:"id"`
	Name    string                  `json:"name"`
	BuiltIn *models.RoleBuiltIn     `json:"builtIn"`
	Sources []models.UserRoleSource `json:"sources"`
}

type adminUserProviderRow struct {
	UserId       string `gorm:"column:user_id"`
	ProviderName string `gorm:"column:provider_name"`
}

func toAdminUserResponse(user *models.User, oidcProviderNames []string, roles []userRoleResponse) adminUserResponse {
	if roles == nil {
		roles = []userRoleResponse{}
	}
	return adminUserResponse{
		Id:                     user.Id,
		Name:                   user.Name,
		Email:                  user.Email,
		Roles:                  roles,
		Providers:              buildAuthProviderNames(user.PasswordHash, oidcProviderNames),
		PasswordChangeRequired: user.PasswordChangeRequired,
		CreatedAt:              user.CreatedAt.Format(time.RFC3339),
		UpdatedAt:              user.UpdatedAt.Format(time.RFC3339),
	}
}

func toAdminUserWithGeneratedPasswordResponse(user *models.User, oidcProviderNames []string, roles []userRoleResponse, generatedPassword string) adminUserWithGeneratedPasswordResponse {
	return adminUserWithGeneratedPasswordResponse{
		adminUserResponse: toAdminUserResponse(user, oidcProviderNames, roles),
		GeneratedPassword: generatedPassword,
	}
}

func AdminListUsersHandler(c *gin.Context) {
	var users []models.User
	if err := db.DB.WithContext(c.Request.Context()).Order("created_at ASC").Find(&users).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	userIds := make([]string, 0, len(users))
	for i := range users {
		userIds = append(userIds, users[i].Id)
	}

	oidcProviderNamesByUserId, err := loadOIDCProviderNamesByUserId(c, userIds)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	rolesByUserId, err := loadRolesByUserId(c.Request.Context(), userIds)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	response := make([]adminUserResponse, 0, len(users))
	for i := range users {
		response = append(response, toAdminUserResponse(&users[i], oidcProviderNamesByUserId[users[i].Id], rolesByUserId[users[i].Id]))
	}

	c.JSON(http.StatusOK, response)
}

func AdminGetUserHandler(c *gin.Context) {
	id := c.Param("id")

	var user models.User
	if err := db.DB.WithContext(c.Request.Context()).Where("id = ?", id).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	response, err := buildAdminUserResponse(c, &user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusOK, response)
}

func AdminCreateUserHandler(c *gin.Context) {
	var req adminCreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: name and email are required and must be valid"})
		return
	}

	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	roleIds := uniqueIds(req.RoleIds)

	generatedPassword, err := auth.GenerateRandomPassword()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	hash, err := auth.HashPassword(generatedPassword)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	user := models.User{
		Name:                   req.Name,
		Email:                  req.Email,
		PasswordHash:           &hash,
		PasswordChangeRequired: true,
	}

	ctx := c.Request.Context()
	err = db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if req.RoleIds == nil {
			role, err := rbac.BuiltInRole(ctx, tx, models.RoleBuiltInUser)
			if err != nil {
				return err
			}
			roleIds = []string{role.Id}
		} else if err := checkRolesExist(ctx, tx, roleIds); err != nil {
			return err
		}
		if err := gorm.G[models.User](tx).Create(ctx, &user); err != nil {
			return err
		}
		_, err := rbac.SetUserRoles(ctx, tx, user.Id, models.UserRoleSourceManual, roleIds)
		return err
	})
	if err != nil {
		if errors.Is(err, errUnknownRole) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if isUniqueConstraintError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "a user with this email already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	rolesByUserId, err := loadRolesByUserId(ctx, []string{user.Id})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	utils.RecordAuditLog(c, "created", "user", user.Id)

	c.JSON(http.StatusCreated, toAdminUserWithGeneratedPasswordResponse(&user, nil, rolesByUserId[user.Id], generatedPassword))
	sse.PublishUpdate(sse.TopicAdminUsers, sse.TopicAdminRoles)
}

func AdminUpdateUserHandler(c *gin.Context) {
	id := c.Param("id")

	var req adminUpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: name and email are required and must be valid"})
		return
	}

	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	roleIds := uniqueIds(req.RoleIds)

	ctx := c.Request.Context()

	var user models.User
	if err := db.DB.WithContext(ctx).Where("id = ?", id).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	// The profile of users without a password is managed by their OIDC provider,
	// only their roles can be changed. Their name and email don't have to be
	// sent, and the provider may have set values the validation would reject.
	if user.PasswordHash == nil {
		if (req.Name != "" && req.Name != user.Name) || (req.Email != "" && !strings.EqualFold(req.Email, user.Email)) || req.ResetPassword {
			c.JSON(http.StatusBadRequest, gin.H{"error": "cannot update a managed user"})
			return
		}
	} else {
		if err := binding.Validator.ValidateStruct(adminUserProfile{Name: req.Name, Email: req.Email}); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: name and email are required and must be valid"})
			return
		}
		if user.Email != req.Email {
			user.EmailUnconfirmed = false
		}
		user.Name = req.Name
		user.Email = req.Email
	}

	generatedPassword := ""
	if req.ResetPassword {
		var genErr error
		generatedPassword, genErr = auth.GenerateRandomPassword()
		if genErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			return
		}

		hash, err := auth.HashPassword(generatedPassword)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			return
		}
		user.PasswordHash = &hash
		user.PasswordChangeRequired = true
		user.TokenVersion++
	}

	rolesChanged := false
	err := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := checkRolesExist(ctx, tx, roleIds); err != nil {
			return err
		}
		if err := tx.Save(&user).Error; err != nil {
			return err
		}
		return keepLastAdmin(ctx, tx, func() error {
			var err error
			rolesChanged, err = rbac.SetUserRoles(ctx, tx, user.Id, models.UserRoleSourceManual, roleIds)
			return err
		})
	})
	if err != nil {
		if errors.Is(err, errLastAdmin) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "cannot demote the last admin user"})
			return
		}
		if errors.Is(err, errUnknownRole) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if isUniqueConstraintError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "a user with this email already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	response, err := buildAdminUserResponse(c, &user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	utils.RecordAuditLog(c, "updated", "user", id)
	if rolesChanged {
		utils.RecordAuditLog(c, "roles-updated", "user", id)
	}

	c.JSON(http.StatusOK, adminUserWithGeneratedPasswordResponse{adminUserResponse: response, GeneratedPassword: generatedPassword})
	sse.PublishUpdate(sse.TopicAdminUsers, sse.TopicAdminRoles, sse.TopicProfile)
}

func AdminDeleteUserHandler(c *gin.Context) {
	id := c.Param("id")
	ctx := c.Request.Context()

	err := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return keepLastAdmin(ctx, tx, func() error {
			rowsAffected, err := gorm.G[models.User](tx).Where("id = ?", id).Delete(ctx)
			if err != nil {
				return err
			}
			if rowsAffected == 0 {
				return gorm.ErrRecordNotFound
			}
			return nil
		})
	})
	if err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		case errors.Is(err, errLastAdmin):
			c.JSON(http.StatusBadRequest, gin.H{"error": "cannot delete the last admin user"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}

	utils.RecordAuditLog(c, "deleted", "user", id)

	c.JSON(http.StatusOK, gin.H{"message": "user deleted"})
	sse.PublishUpdate(sse.TopicAdminUsers, sse.TopicAdminRoles)
}

var errLastAdmin = errors.New("at least one admin user is required")

// keepLastAdmin runs a change that can remove admins and fails it when it would
// leave the hub without anyone who can manage it.
func keepLastAdmin(ctx context.Context, tx *gorm.DB, change func() error) error {
	before, err := rbac.CountAdmins(ctx, tx)
	if err != nil {
		return err
	}
	if err := change(); err != nil {
		return err
	}
	after, err := rbac.CountAdmins(ctx, tx)
	if err != nil {
		return err
	}
	if before > 0 && after == 0 {
		return errLastAdmin
	}
	return nil
}

var errUnknownRole = errors.New("role not found")

func checkRolesExist(ctx context.Context, tx *gorm.DB, roleIds []string) error {
	if len(roleIds) == 0 {
		return nil
	}
	count, err := gorm.G[models.Role](tx).Where("id IN ?", roleIds).Count(ctx, "*")
	if err != nil {
		return err
	}
	if int(count) != len(roleIds) {
		return errUnknownRole
	}
	return nil
}

func buildAdminUserResponse(c *gin.Context, user *models.User) (adminUserResponse, error) {
	oidcProviderNamesByUserId, err := loadOIDCProviderNamesByUserId(c, []string{user.Id})
	if err != nil {
		return adminUserResponse{}, err
	}
	rolesByUserId, err := loadRolesByUserId(c.Request.Context(), []string{user.Id})
	if err != nil {
		return adminUserResponse{}, err
	}
	return toAdminUserResponse(user, oidcProviderNamesByUserId[user.Id], rolesByUserId[user.Id]), nil
}

// loadRolesByUserId returns the roles of the users, sorted with the built-in roles first.
func loadRolesByUserId(ctx context.Context, userIds []string) (map[string][]userRoleResponse, error) {
	rolesByUserId := make(map[string][]userRoleResponse, len(userIds))
	if len(userIds) == 0 {
		return rolesByUserId, nil
	}

	assignments, err := gorm.G[models.UserRole](db.DB).
		Preload("Role", nil).
		Where("user_id IN ?", userIds).
		Find(ctx)
	if err != nil {
		return nil, err
	}

	// A user can have a role from several sources, which are merged into one role.
	type roleKey struct{ userId, roleId string }
	indexOf := make(map[roleKey]int, len(assignments))
	for i := range assignments {
		a := &assignments[i]
		if a.Role == nil {
			continue
		}
		key := roleKey{a.UserId, a.RoleId}
		idx, ok := indexOf[key]
		if !ok {
			idx = len(rolesByUserId[a.UserId])
			indexOf[key] = idx
			rolesByUserId[a.UserId] = append(rolesByUserId[a.UserId], userRoleResponse{Id: a.RoleId, Name: a.Role.Name, BuiltIn: a.Role.BuiltIn})
		}
		roles := rolesByUserId[a.UserId]
		roles[idx].Sources = append(roles[idx].Sources, a.Source)
	}

	for userId, roles := range rolesByUserId {
		for i := range roles {
			slices.Sort(roles[i].Sources)
		}
		slices.SortFunc(roles, compareUserRoles)
		rolesByUserId[userId] = roles
	}

	return rolesByUserId, nil
}

func compareUserRoles(a, b userRoleResponse) int {
	if (a.BuiltIn != nil) != (b.BuiltIn != nil) {
		if a.BuiltIn != nil {
			return -1
		}
		return 1
	}
	if a.BuiltIn != nil && *a.BuiltIn != *b.BuiltIn {
		// Admin before user
		return strings.Compare(string(*a.BuiltIn), string(*b.BuiltIn))
	}
	return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
}

func loadOIDCProviderNamesByUserId(c *gin.Context, userIds []string) (map[string][]string, error) {
	providerNamesByUserId := make(map[string][]string, len(userIds))
	for _, userId := range userIds {
		providerNamesByUserId[userId] = make([]string, 0)
	}

	if len(userIds) == 0 {
		return providerNamesByUserId, nil
	}

	var rows []adminUserProviderRow
	err := db.DB.WithContext(c.Request.Context()).
		Table("user_oidc_identities AS uoi").
		Select("uoi.user_id, op.name AS provider_name").
		Joins("JOIN oidc_providers AS op ON op.id = uoi.provider_id").
		Where("uoi.user_id IN ?", userIds).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	for _, row := range rows {
		providerName := strings.TrimSpace(row.ProviderName)
		if providerName == "" {
			continue
		}
		providerNamesByUserId[row.UserId] = append(providerNamesByUserId[row.UserId], providerName)
	}

	return providerNamesByUserId, nil
}

func buildAuthProviderNames(passwordHash *string, oidcProviderNames []string) []string {
	seen := make(map[string]struct{}, len(oidcProviderNames)+1)
	providerNames := make([]string, 0, len(oidcProviderNames)+1)

	if passwordHash != nil {
		providerNames = append(providerNames, "password")
		seen["password"] = struct{}{}
	}

	uniqueOIDCProviderNames := make([]string, 0, len(oidcProviderNames))
	for _, providerName := range oidcProviderNames {
		providerName = strings.TrimSpace(providerName)
		if providerName == "" {
			continue
		}
		if _, ok := seen[providerName]; ok {
			continue
		}
		seen[providerName] = struct{}{}
		uniqueOIDCProviderNames = append(uniqueOIDCProviderNames, providerName)
	}

	sort.Strings(uniqueOIDCProviderNames)

	providerNames = append(providerNames, uniqueOIDCProviderNames...)
	return providerNames
}

func isUniqueConstraintError(err error) bool {
	return errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(strings.ToLower(err.Error()), "unique constraint failed")
}
