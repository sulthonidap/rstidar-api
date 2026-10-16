package handlers

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"medsend-api/internal/enums"
	"medsend-api/internal/imageproc"
	"medsend-api/internal/middleware"
	"medsend-api/internal/models"
	"medsend-api/internal/services"
	"medsend-api/internal/storage"
)

const (
	maxPodPhotos     = 1
	maxPodPhotoBytes = 8 << 20 // per upload; the app already sends ~200–500 KB, the server re-shrinks anyway
)

// Accepted uploads (sniffed from the bytes); all are stored as JPEG.
var podPhotoTypes = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true}

// withPhotoURLs turns the stored photo keys into loadable URLs; photos from
// before thumbnails existed use the full photo as their thumbnail.
func withPhotoURLs(store storage.Storage, d *models.OrderDelivery) {
	if d == nil {
		return
	}
	d.PhotoURLs = make([]string, 0, len(d.Photos))
	d.ThumbURLs = make([]string, 0, len(d.Photos))
	for i, key := range d.Photos {
		full := store.URL(key)
		d.PhotoURLs = append(d.PhotoURLs, full)
		if i < len(d.Thumbs) {
			d.ThumbURLs = append(d.ThumbURLs, store.URL(d.Thumbs[i]))
		} else {
			d.ThumbURLs = append(d.ThumbURLs, full)
		}
	}
}

// Deliver — POST /api/courier/orders/:id/deliver (multipart/form-data):
// photos (1 file), receiver_name, receiver_relation, note?, lat?, lng?, accuracy?.
// Finishing an order requires this proof of delivery: it saves the photos,
// records who received the package and where, and moves the order to Selesai.
func (h *CourierAppHandler) Deliver(c *fiber.Ctx) error {
	order, err := h.findMine(c)
	if err != nil {
		return err
	}
	if order.Status != enums.OrderStatusOnDelivery {
		return fiber.NewError(fiber.StatusUnprocessableEntity,
			"order berstatus "+order.Status.Label()+", bukan dalam pengiriman")
	}
	// Ops follow the courier's saved route: a package is only finished once it's on it.
	if !inOpenRoute(h.DB, middleware.CurrentCourier(c).ID, order.ID) {
		return fiber.NewError(fiber.StatusUnprocessableEntity,
			"simpan rute dulu di Urutan antar (paket ini belum ada di rute yang kamu simpan)")
	}

	name := strings.TrimSpace(c.FormValue("receiver_name"))
	relation := c.FormValue("receiver_relation")
	if name == "" || len(name) > 100 {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "nama penerima wajib diisi (maks. 100 karakter)")
	}
	if _, ok := models.ReceiverRelations[relation]; !ok {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "pilih hubungan penerima")
	}
	var note *string
	if n := strings.TrimSpace(c.FormValue("note")); n != "" {
		if len(n) > 500 {
			return fiber.NewError(fiber.StatusUnprocessableEntity, "catatan maksimal 500 karakter")
		}
		note = &n
	}
	lat, lng := formFloat(c, "lat"), formFloat(c, "lng")
	if !validPoint(lat, lng) {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "lokasi GPS tidak valid")
	}
	var accuracy *int
	if a := formFloat(c, "accuracy"); a != nil && *a >= 0 {
		v := int(*a + 0.5)
		accuracy = &v
	}

	form, err := c.MultipartForm()
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "kirim sebagai multipart/form-data")
	}
	files := form.File["photos"]
	if len(files) == 0 || len(files) > maxPodPhotos {
		return fiber.NewError(fiber.StatusUnprocessableEntity, "wajib 1 foto bukti terima")
	}

	ctx := c.UserContext()
	keys, thumbs, err := h.savePodPhotos(ctx, order, files)
	if err != nil {
		return err
	}

	user := middleware.CurrentUser(c)
	now := time.Now()
	statusNote := fmt.Sprintf("Diterima oleh %s (%s)", name, strings.ToLower(models.ReceiverRelations[relation]))
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&models.OrderDelivery{
			OrderID: order.ID, ReceiverName: name, ReceiverRelation: relation, Note: note,
			Photos: keys, Thumbs: thumbs, Lat: lat, Lng: lng, AccuracyM: accuracy,
			DeliveredBy: &user.ID, DeliveredAt: now,
		}).Error; err != nil {
			return err
		}
		return (&services.OrderService{DB: tx}).Transition(order, enums.OrderStatusDone, &user.ID, &statusNote)
	})
	if err != nil {
		for _, k := range append(keys, thumbs...) {
			_ = h.Store.Delete(ctx, k)
		}
		return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
	}
	// That may have been the route's last package.
	closeRouteIfDone(h.DB, middleware.CurrentCourier(c).ID)

	updated, err := h.findMine(c)
	if err != nil {
		return err
	}
	withPhotoURLs(h.Store, updated.Delivery)
	return c.JSON(fiber.Map{"data": updated})
}

// savePodPhotos checks each upload is a real image of acceptable size, then
// stores a normalized copy (upright, max 1600px, JPEG without EXIF) and a
// square thumbnail under pod/YYYY/MM/. On any failure the files already saved
// are removed. Returns the photo keys and the thumbnail keys (same order).
func (h *CourierAppHandler) savePodPhotos(ctx context.Context, order *models.Order, files []*multipart.FileHeader) ([]string, []string, error) {
	keys := make([]string, 0, len(files))
	thumbs := make([]string, 0, len(files))
	fail := func(err error) ([]string, []string, error) {
		for _, k := range append(keys, thumbs...) {
			_ = h.Store.Delete(ctx, k)
		}
		return nil, nil, err
	}
	for i, fh := range files {
		if fh.Size > maxPodPhotoBytes {
			return fail(fiber.NewError(fiber.StatusUnprocessableEntity, fmt.Sprintf("foto %d terlalu besar (maks. 8 MB)", i+1)))
		}
		f, err := fh.Open()
		if err != nil {
			return fail(fiber.NewError(fiber.StatusBadRequest, "foto tidak bisa dibaca"))
		}
		head := make([]byte, 512)
		n, _ := f.Read(head)
		if _, ok := podPhotoTypes[http.DetectContentType(head[:n])]; !ok {
			f.Close()
			return fail(fiber.NewError(fiber.StatusUnprocessableEntity, fmt.Sprintf("foto %d harus JPG/PNG/WEBP", i+1)))
		}
		if _, err := f.Seek(0, 0); err != nil {
			f.Close()
			return fail(err)
		}
		img, err := imageproc.Process(f)
		f.Close()
		if err != nil {
			return fail(fiber.NewError(fiber.StatusUnprocessableEntity, fmt.Sprintf("foto %d tidak bisa diproses: %v", i+1, err)))
		}

		base := fmt.Sprintf("pod/%s/%s-%s", time.Now().Format("2006/01"), order.Code, randomHex(8))
		for _, file := range []struct {
			key  string
			data []byte
			list *[]string
		}{
			{base + ".jpg", img.Photo, &keys},
			{base + "_thumb.jpg", img.Thumb, &thumbs},
		} {
			if err := h.Store.Save(ctx, file.key, bytes.NewReader(file.data), int64(len(file.data)), "image/jpeg"); err != nil {
				return fail(fiber.NewError(fiber.StatusInternalServerError, "gagal menyimpan foto"))
			}
			*file.list = append(*file.list, file.key)
		}
	}
	return keys, thumbs, nil
}

func formFloat(c *fiber.Ctx, key string) *float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(c.FormValue(key)), 64)
	if err != nil {
		return nil
	}
	return &v
}

// randomHex makes photo URLs unguessable (they're served without login).
func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
