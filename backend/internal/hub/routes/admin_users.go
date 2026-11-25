package routes

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/passkeys"
	"github.com/OrcaCD/orca-cd/internal/hub/rbac"
	"github.com/OrcaCD/orca-cd/internal/hub/restapi"
	"github.com/OrcaCD/orca-cd/internal/hub/sse"
	"github.com/OrcaCD/orca-cd/internal/hub/utils"
	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin/binding"
	"gorm.io/gorm"
)

type adminUserResponse struct {
	Id        string             `json:"id" required:"true"`
	Name      string             `json:"name" required:"true"`
	Email     string             `json:"email" required:"true"`
	Roles     []userRoleResponse `json:"roles" required:"true"`
	Providers []string           `json:"providers" required:"true" doc:"How the user signs in: \"passkey\" for local users, then the names of their OIDC providers"`
	IsLocal   bool               `json:"isLocal" required:"true" doc:"Whether the user signs in with passkeys. The profile of other users is managed by their OIDC provider."`
	CreatedAt string             `json:"createdAt" required:"true" format:"date-time"`
	UpdatedAt string             `json:"updatedAt" required:"true" format:"date-time"`
}

type adminUserWithEnrollmentResponse struct {
	adminUserResponse
	EnrollmentToken string `json:"enrollmentToken,omitempty" doc:"The token with which the user registers a passkey. Only returned once."`
}

type adminCreateUserRequest struct {
	Name  string `json:"name" required:"true" minLength:"3" maxLength:"64"`
	Email string `json:"email" required:"true"`
	// Without roleIds, the user gets the default role
	RoleIds []string `json:"roleIds" maxItems:"100" doc:"Leave it out to give the user the built-in user role"`
}

type adminUpdateUserRequest struct {
	// Name and email are validated by adminUserProfile, unless the profile is
	// managed by an OIDC provider.
	Name  string `json:"name" doc:"Required for local users, 3 to 64 characters"`
	Email string `json:"email" doc:"Required for local users"`
	// Without roleIds, the roles are left unchanged
	RoleIds       []string `json:"roleIds" maxItems:"100" doc:"The roles an admin assigns. Leave it out to keep them."`
	ResetPasskeys bool     `json:"resetPasskeys" doc:"Removes the passkeys of a local user and returns a new enrollment token"`
}

type adminUserProfile struct {
	Name  string `binding:"required,min=3,max=64"`
	Email string `binding:"required,email"`
}

// emailAddress is checked with Gin's email rule instead of format:"email",
// which also accepts domains without a dot, like john@localhost.
type emailAddress struct {
	Email string `binding:"required,email"`
}

// userRoleResponse is a role of a user. Sources tells whether an admin assigned
// it, it comes from an OIDC group, or both.
type userRoleResponse struct {
	Id      string                  `json:"id" required:"true"`
	Name    string                  `json:"name" required:"true"`
	BuiltIn *models.RoleBuiltIn     `json:"builtIn" required:"true" nullable:"true" enum:"admin,user" doc:"Which built-in role it is, null for roles admins created"`
	Sources []models.UserRoleSource `json:"sources" required:"true" enum:"manual,oidc" doc:"Whether an admin assigned the role, the user gets it from an OIDC group, or both"`
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
		Id:        user.Id,
		Name:      user.Name,
		Email:     user.Email,
		Roles:     roles,
		Providers: buildAuthProviderNames(user.LocalAuthEnabled, oidcProviderNames),
		IsLocal:   user.LocalAuthEnabled,
		CreatedAt: user.CreatedAt.Format(time.RFC3339),
		UpdatedAt: user.UpdatedAt.Format(time.RFC3339),
	}
}

func toAdminUserWithEnrollmentTokenResponse(user *models.User, oidcProviderNames []string, roles []userRoleResponse, enrollmentToken string) adminUserWithEnrollmentResponse {
	return adminUserWithEnrollmentResponse{
		adminUserResponse: toAdminUserResponse(user, oidcProviderNames, roles),
		EnrollmentToken:   enrollmentToken,
	}
}

type adminUserOutput struct {
	Body adminUserResponse
}

type adminUserWithEnrollmentOutput struct {
	Body adminUserWithEnrollmentResponse
}

var AdminListUsersHandler = restapi.Handler(huma.Operation{
	OperationID: "admin-list-users",
	Method:      http.MethodGet,
	Path:        "/admin/users",
	Summary:     "List users",
	Description: "Lists every user with their roles and how they sign in. " + requiresAdmin,
	Tags:        []string{tagAdmin},
	Security:    restapi.Authenticated,
	Errors:      []int{http.StatusForbidden},
}, func(ctx context.Context, _ *struct{}) (*struct{ Body []adminUserResponse }, error) {
	users, err := gorm.G[models.User](db.DB).Order("created_at ASC").Find(ctx)
	if err != nil {
		return nil, internalError()
	}

	userIds := make([]string, 0, len(users))
	for i := range users {
		userIds = append(userIds, users[i].Id)
	}

	oidcProviderNamesByUserId, err := loadOIDCProviderNamesByUserId(ctx, userIds)
	if err != nil {
		return nil, internalError()
	}

	rolesByUserId, err := loadRolesByUserId(ctx, userIds)
	if err != nil {
		return nil, internalError()
	}

	response := make([]adminUserResponse, 0, len(users))
	for i := range users {
		response = append(response, toAdminUserResponse(&users[i], oidcProviderNamesByUserId[users[i].Id], rolesByUserId[users[i].Id]))
	}

	return &struct{ Body []adminUserResponse }{Body: response}, nil
})

var AdminGetUserHandler = restapi.Handler(huma.Operation{
	OperationID: "admin-get-user",
	Method:      http.MethodGet,
	Path:        "/admin/users/{id}",
	Summary:     "Get a user",
	Description: requiresAdmin,
	Tags:        []string{tagAdmin},
	Security:    restapi.Authenticated,
	Errors:      []int{http.StatusNotFound, http.StatusForbidden},
}, func(ctx context.Context, input *idInput) (*adminUserOutput, error) {
	user, err := gorm.G[models.User](db.DB).Where("id = ?", input.ID).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, huma.Error404NotFound("user not found")
		}
		return nil, internalError()
	}

	response, err := buildAdminUserResponse(ctx, &user)
	if err != nil {
		return nil, internalError()
	}

	return &adminUserOutput{Body: response}, nil
})

var AdminCreateUserHandler = restapi.Handler(huma.Operation{
	OperationID:   "admin-create-user",
	Method:        http.MethodPost,
	Path:          "/admin/users",
	DefaultStatus: http.StatusCreated,
	Summary:       "Create a user",
	Description: "Creates a user who signs in with a passkey and returns the enrollment token to register it with, which is only shown once. " +
		requiresAdmin,
	Tags:     []string{tagAdmin},
	Security: restapi.Authenticated,
	Errors:   []int{http.StatusBadRequest, http.StatusConflict, http.StatusForbidden},
}, func(ctx context.Context, input *struct{ Body adminCreateUserRequest }) (*adminUserWithEnrollmentOutput, error) {
	req := input.Body
	if err := binding.Validator.ValidateStruct(emailAddress{Email: req.Email}); err != nil {
		return nil, huma.Error400BadRequest("invalid request: name and email are required and must be valid")
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	roleIds := uniqueIds(req.RoleIds)

	user := models.User{Name: req.Name, Email: req.Email, LocalAuthEnabled: true}
	enrollmentToken := ""
	err := db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
		if _, err := rbac.SetUserRoles(ctx, tx, user.Id, models.UserRoleSourceManual, roleIds); err != nil {
			return err
		}
		var err error
		enrollmentToken, err = passkeys.IssueEnrollment(ctx, tx, &user)
		return err
	})
	if err != nil {
		if errors.Is(err, errUnknownRole) {
			return nil, huma.Error400BadRequest(err.Error())
		}
		if isUniqueConstraintError(err) {
			return nil, huma.Error409Conflict("a user with this email already exists")
		}
		return nil, internalError()
	}

	rolesByUserId, err := loadRolesByUserId(ctx, []string{user.Id})
	if err != nil {
		return nil, internalError()
	}

	utils.RecordAuditLog(restapi.GinContext(ctx), "created", "user", user.Id)
	sse.PublishUpdate(sse.TopicAdminUsers, sse.TopicAdminRoles)

	return &adminUserWithEnrollmentOutput{Body: toAdminUserWithEnrollmentTokenResponse(&user, nil, rolesByUserId[user.Id], enrollmentToken)}, nil
})

var AdminUpdateUserHandler = restapi.Handler(huma.Operation{
	OperationID: "admin-update-user",
	Method:      http.MethodPut,
	Path:        "/admin/users/{id}",
	Summary:     "Update a user",
	Description: "Only the roles of users managed by an OIDC provider can be changed. The last admin can't lose the admin role. " +
		requiresAdmin,
	Tags:     []string{tagAdmin},
	Security: restapi.Authenticated,
	Errors:   []int{http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusForbidden},
}, func(ctx context.Context, input *struct {
	ID   string `path:"id" doc:"The ID of the user"`
	Body adminUpdateUserRequest
}) (*adminUserWithEnrollmentOutput, error) {
	id, req := input.ID, input.Body

	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	roleIds := uniqueIds(req.RoleIds)

	user, err := gorm.G[models.User](db.DB).Where("id = ?", id).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, huma.Error404NotFound("user not found")
		}
		return nil, internalError()
	}

	// The profile of users without local authentication is managed by their OIDC provider,
	// only their roles can be changed. Their name and email don't have to be
	// sent, and the provider may have set values the validation would reject.
	if !user.LocalAuthEnabled {
		if (req.Name != "" && req.Name != user.Name) || (req.Email != "" && !strings.EqualFold(req.Email, user.Email)) || req.ResetPasskeys {
			return nil, huma.Error400BadRequest("cannot update a managed user")
		}
	} else {
		if err := binding.Validator.ValidateStruct(adminUserProfile{Name: req.Name, Email: req.Email}); err != nil {
			return nil, huma.Error400BadRequest("invalid request: name and email are required and must be valid")
		}
		if user.Email != req.Email {
			user.EmailUnconfirmed = false
		}
		user.Name = req.Name
		user.Email = req.Email
	}

	enrollmentToken := ""

	rolesChanged := false
	err = db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if user.LocalAuthEnabled {
			user.UpdatedAt = time.Now()
			// Only the columns this handler changes, so a concurrent change of
			// other columns is not overwritten and a deleted user is not recreated.
			rows, err := gorm.G[models.User](tx).Where("id = ?", user.Id).
				Select("name", "email", "email_unconfirmed", "updated_at").
				Updates(ctx, user)
			if err != nil {
				return err
			}
			if rows == 0 {
				return gorm.ErrRecordNotFound
			}
		}
		if req.ResetPasskeys {
			var err error
			enrollmentToken, err = passkeys.ResetLocal(ctx, tx, &user)
			if err != nil {
				return err
			}
		}
		if req.RoleIds == nil {
			return nil
		}
		if err := checkRolesExist(ctx, tx, roleIds); err != nil {
			return err
		}
		return keepLastAdmin(ctx, tx, func() error {
			var err error
			rolesChanged, err = rbac.SetUserRoles(ctx, tx, user.Id, models.UserRoleSourceManual, roleIds)
			return err
		})
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, huma.Error404NotFound("user not found")
		}
		if errors.Is(err, errLastAdmin) {
			return nil, huma.Error400BadRequest("cannot demote the last admin user")
		}
		if errors.Is(err, errUnknownRole) {
			return nil, huma.Error400BadRequest(err.Error())
		}
		if isUniqueConstraintError(err) {
			return nil, huma.Error409Conflict("a user with this email already exists")
		}
		return nil, internalError()
	}

	response, err := buildAdminUserResponse(ctx, &user)
	if err != nil {
		return nil, internalError()
	}

	c := restapi.GinContext(ctx)
	utils.RecordAuditLog(c, "updated", "user", id)
	if rolesChanged {
		utils.RecordAuditLog(c, "roles-updated", "user", id)
	}
	sse.PublishUpdate(sse.TopicAdminUsers, sse.TopicAdminRoles, sse.TopicProfileOf(id))

	return &adminUserWithEnrollmentOutput{Body: adminUserWithEnrollmentResponse{adminUserResponse: response, EnrollmentToken: enrollmentToken}}, nil
})

var AdminDeleteUserHandler = restapi.Handler(huma.Operation{
	OperationID: "admin-delete-user",
	Method:      http.MethodDelete,
	Path:        "/admin/users/{id}",
	Summary:     "Delete a user",
	Description: "The last admin can't be deleted. " + requiresAdmin,
	Tags:        []string{tagAdmin},
	Security:    restapi.Authenticated,
	Errors:      []int{http.StatusBadRequest, http.StatusNotFound, http.StatusForbidden},
}, func(ctx context.Context, input *idInput) (*restapi.MessageOutput, error) {
	id := input.ID

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
			return nil, huma.Error404NotFound("user not found")
		case errors.Is(err, errLastAdmin):
			return nil, huma.Error400BadRequest("cannot delete the last admin user")
		default:
			return nil, internalError()
		}
	}

	utils.RecordAuditLog(restapi.GinContext(ctx), "deleted", "user", id)
	sse.PublishUpdate(sse.TopicAdminUsers, sse.TopicAdminRoles, sse.TopicProfileOf(id))

	return restapi.Message("user deleted"), nil
})

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

func buildAdminUserResponse(ctx context.Context, user *models.User) (adminUserResponse, error) {
	oidcProviderNamesByUserId, err := loadOIDCProviderNamesByUserId(ctx, []string{user.Id})
	if err != nil {
		return adminUserResponse{}, err
	}
	rolesByUserId, err := loadRolesByUserId(ctx, []string{user.Id})
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

func loadOIDCProviderNamesByUserId(ctx context.Context, userIds []string) (map[string][]string, error) {
	providerNamesByUserId := make(map[string][]string, len(userIds))
	for _, userId := range userIds {
		providerNamesByUserId[userId] = make([]string, 0)
	}

	if len(userIds) == 0 {
		return providerNamesByUserId, nil
	}

	var rows []adminUserProviderRow
	err := db.DB.WithContext(ctx).
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

func buildAuthProviderNames(localAuth bool, oidcProviderNames []string) []string {
	seen := make(map[string]struct{}, len(oidcProviderNames)+1)
	providerNames := make([]string, 0, len(oidcProviderNames)+1)

	if localAuth {
		providerNames = append(providerNames, "passkey")
		seen["passkey"] = struct{}{}
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
