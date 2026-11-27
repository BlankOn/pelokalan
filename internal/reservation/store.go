package reservation

import (
	"context"

	"pelokalan/internal/models"
)

// ReservationRepository stores module reservations (module_reservations)
type Repository interface {
	// Active lists a project's held modules with the holder and their last save in the module
	Active(ctx context.Context, projectID int64) ([]*models.ModuleReservation, error)
	// ActiveFor returns the reservation of one module (ErrNotFound when it is free)
	ActiveFor(ctx context.Context, projectID int64, module string) (*models.ModuleReservation, error)
	// Create reserves a module; ErrConflict when someone holds it already
	Create(ctx context.Context, projectID int64, module string, userID int64) error
	// Release ends a reservation (by: who released it, nil = automatic)
	Release(ctx context.Context, id int64, by *int64, reason string) error
	// Expiring lists the held modules of projects with reservations on and an expiry, for reminders and expiry
	Expiring(ctx context.Context) ([]*models.ModuleReservation, error)
	SetReminded(ctx context.Context, id int64) error
}
