package handlers

import (
	"github.com/gofiber/fiber/v2"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"medsend-api/internal/enums"
	"medsend-api/internal/models"
)

type UserHandler struct {
	DB *gorm.DB
}

func (h *UserHandler) Index(c *fiber.Ctx) error {
	var users []models.User
	if err := h.DB.Preload("Role").Preload("Organization").Order("name").Find(&users).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"data": users})
}

type userRequest struct {
	Name     string  `json:"name" validate:"required"`
	Email    string  `json:"email" validate:"required,email"`
	Phone    *string `json:"phone"`
	Password string  `json:"password"`
	RoleID   *uint   `json:"role_id" validate:"required"`
	IsActive *bool   `json:"is_active"`
}

func (h *UserHandler) Store(c *fiber.Ctx) error {
	var req userRequest
	if err := c.BodyParser(&req); err != nil || req.Name == "" || req.Email == "" || req.Password == "" {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "nama, email, dan password wajib diisi")
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "gagal hash password")
	}

	user := models.User{
		Name:     req.Name,
		Email:    req.Email,
		Phone:    req.Phone,
		Password: string(hashed),
		RoleID:   req.RoleID,
		IsActive: true,
	}
	if req.IsActive != nil {
		user.IsActive = *req.IsActive
	}

	if err := h.DB.Create(&user).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	h.DB.Preload("Role").Preload("Organization").First(&user, user.ID)
	if err := h.ensureCourier(&user); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": user})
}

func (h *UserHandler) Update(c *fiber.Ctx) error {
	var user models.User
	if err := h.DB.First(&user, "id = ?", c.Params("id")).Error; err != nil {
		return fiber.NewError(fiber.StatusNotFound, "user tidak ditemukan")
	}

	var req userRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "payload tidak valid")
	}

	if req.Name != "" {
		user.Name = req.Name
	}
	if req.Email != "" {
		user.Email = req.Email
	}
	user.Phone = req.Phone
	if req.Password != "" {
		hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, "gagal hash password")
		}
		user.Password = string(hashed)
	}
	if req.RoleID != nil {
		user.RoleID = req.RoleID
	}
	if req.IsActive != nil {
		user.IsActive = *req.IsActive
	}

	if err := h.DB.Save(&user).Error; err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	h.DB.Preload("Role").Preload("Organization").First(&user, user.ID)
	if err := h.ensureCourier(&user); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"data": user})
}

// ensureCourier membuatkan data kurir untuk user ber-role Kurir yang belum
// punya, supaya bisa langsung login di aplikasi kurir.
func (h *UserHandler) ensureCourier(user *models.User) error {
	if !user.HasRole(enums.RoleKurir) {
		return nil
	}
	var count int64
	if err := h.DB.Model(&models.Courier{}).Where("user_id = ?", user.ID).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	return h.DB.Create(&models.Courier{
		UserID:         user.ID,
		OrganizationID: user.OrganizationID,
		Phone:          user.Phone,
		IsActive:       true,
	}).Error
}
