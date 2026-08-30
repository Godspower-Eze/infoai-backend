package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/Godspower-Eze/infoai-backend/internal/auth"
	xintegration "github.com/Godspower-Eze/infoai-backend/internal/integrations/x"
	"github.com/Godspower-Eze/infoai-backend/internal/posts"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-chi/httprate"
	"github.com/google/uuid"
)

type AuthService interface {
	Register(ctx context.Context, email, password string) (auth.User, error)
	Authenticate(ctx context.Context, email, password string) (auth.User, error)
	User(ctx context.Context, id uuid.UUID) (auth.User, error)
}

type XService interface {
	BeginAuthorization(ctx context.Context) (string, error)
	CompleteAuthorization(ctx context.Context, ownerID uuid.UUID, state, code string) (xintegration.Account, error)
	Accounts(ctx context.Context, ownerID uuid.UUID) ([]xintegration.Account, error)
	Disconnect(ctx context.Context, ownerID, accountID uuid.UUID) error
}

type Readiness interface {
	Ping(ctx context.Context) error
}

type PostService interface {
	Create(context.Context, posts.CreateCommand) (posts.Post, error)
	Get(context.Context, uuid.UUID, uuid.UUID) (posts.Post, error)
	List(context.Context, uuid.UUID) ([]posts.Post, error)
	Update(context.Context, posts.UpdateCommand) (posts.Post, error)
	Delete(context.Context, uuid.UUID, uuid.UUID) error
	RequestDeletion(context.Context, posts.DeleteCommand) (bool, error)
	UploadMedia(context.Context, posts.UploadMediaCommand) (posts.Post, error)
	RemoveMedia(context.Context, posts.RemoveMediaCommand) error
	Publish(context.Context, uuid.UUID, uuid.UUID) (posts.Post, error)
	Schedule(context.Context, uuid.UUID, uuid.UUID, time.Time) (posts.Post, error)
	CancelSchedule(context.Context, uuid.UUID, uuid.UUID) (posts.Post, error)
	ResolveOutcome(context.Context, posts.ResolveOutcomeCommand) (posts.Post, error)
	Retry(context.Context, posts.RetryCommand) (posts.Post, error)
}

type Dependencies struct {
	Auth                 AuthService
	X                    XService
	Posts                PostService
	Sessions             *scs.SessionManager
	Readiness            Readiness
	FrontendOrigin       string
	FrontendXRedirectURL string
	TrustedProxyCIDRs    []string
}

type API struct {
	auth                 AuthService
	x                    XService
	posts                PostService
	sessions             *scs.SessionManager
	readiness            Readiness
	frontendXRedirectURL string
	signinEmails         *fixedWindowLimiter
}

func NewRouter(dependencies Dependencies) (http.Handler, error) {
	if dependencies.Auth == nil || dependencies.X == nil || dependencies.Posts == nil || dependencies.Sessions == nil || dependencies.Readiness == nil {
		return nil, errors.New("HTTP API dependencies must not be nil")
	}
	protection := http.NewCrossOriginProtection()
	if err := protection.AddTrustedOrigin(dependencies.FrontendOrigin); err != nil {
		return nil, err
	}
	api := &API{
		auth:                 dependencies.Auth,
		x:                    dependencies.X,
		posts:                dependencies.Posts,
		sessions:             dependencies.Sessions,
		readiness:            dependencies.Readiness,
		frontendXRedirectURL: dependencies.FrontendXRedirectURL,
		signinEmails:         newFixedWindowLimiter(5, time.Minute),
	}

	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.Recoverer)
	router.Use(middleware.Timeout(30 * time.Second))
	if len(dependencies.TrustedProxyCIDRs) == 0 {
		router.Use(middleware.ClientIPFromRemoteAddr)
	} else {
		router.Use(middleware.ClientIPFromXFF(dependencies.TrustedProxyCIDRs...))
	}
	router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{dependencies.FrontendOrigin},
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowedHeaders:   []string{"Accept", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	}))
	router.Use(protection.Handler)
	router.Use(dependencies.Sessions.LoadAndSave)

	router.Get("/health/live", api.live)
	router.Get("/health/ready", api.ready)
	router.Route("/api/v1", func(router chi.Router) {
		router.Route("/auth", func(router chi.Router) {
			router.With(httprate.LimitBy(5, time.Minute, clientIPRateLimitKey)).Post("/signup", api.signup)
			router.With(httprate.LimitBy(10, time.Minute, clientIPRateLimitKey)).Post("/signin", api.signin)
			router.Post("/signout", api.signout)
			router.With(api.requireUser).Get("/me", api.me)
		})
		router.Route("/integrations/x", func(router chi.Router) {
			router.Use(api.requireUser)
			router.Post("/authorize", api.authorizeX)
			router.Get("/callback", api.xCallback)
			router.Get("/accounts", api.xAccounts)
			router.Delete("/accounts/{accountID}", api.disconnectX)
		})
		router.Route("/posts", func(router chi.Router) {
			router.Use(api.requireUser)
			router.Post("/", api.createPost)
			router.Get("/", api.listPosts)
			router.Get("/{postID}", api.getPost)
			router.Patch("/{postID}", api.updatePost)
			router.Delete("/{postID}", api.deletePost)
			router.Post("/{postID}/publish", api.publishPost)
			router.Post("/{postID}/retry", api.retryPost)
			router.Post("/{postID}/schedule", api.schedulePost)
			router.Post("/{postID}/schedule/cancel", api.cancelPostSchedule)
			router.Post("/{postID}/items/{itemID}/media", api.uploadPostMedia)
			router.Delete("/{postID}/items/{itemID}/media/{mediaID}", api.removePostMedia)
			router.Post("/{postID}/items/{itemID}/resolve-outcome", api.resolvePostOutcome)
		})
	})
	return router, nil
}

func clientIPRateLimitKey(request *http.Request) (string, error) {
	return httprate.CanonicalizeIP(middleware.GetClientIP(request.Context())), nil
}

type userIDContextKey struct{}

func (api *API) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		userID, err := auth.CurrentUserID(request.Context(), api.sessions)
		if err != nil {
			writeError(response, http.StatusUnauthorized, "unauthenticated", "Authentication is required.", nil)
			return
		}
		ctx := context.WithValue(request.Context(), userIDContextKey{}, userID)
		next.ServeHTTP(response, request.WithContext(ctx))
	})
}

func requestUserID(request *http.Request) uuid.UUID {
	return request.Context().Value(userIDContextKey{}).(uuid.UUID)
}
