package egress

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"dosh/internal/domain"
)

const defaultPollInterval = 10 * time.Second

// StoredSettings is the persisted runtime override. Mode "bootstrap" is a
// tombstone marker that returns all replicas to the startup configuration.
type StoredSettings struct {
	Mode       string    `json:"mode"`
	HTTPProxy  string    `json:"httpProxy,omitempty"`
	HTTPSProxy string    `json:"httpsProxy,omitempty"`
	NoProxy    string    `json:"noProxy,omitempty"`
	Username   string    `json:"username,omitempty"`
	Password   string    `json:"password,omitempty"`
	CAPEM      []byte    `json:"caPEM,omitempty"`
	UpdatedAt  time.Time `json:"updatedAt"`
	Revision   string    `json:"-"`
}

type Store interface {
	LoadEgressSettings(ctx context.Context) (StoredSettings, bool, error)
	SaveEgressSettings(ctx context.Context, settings StoredSettings, expectedRevision string) (StoredSettings, error)
}

type UpdateInput struct {
	Mode             string `json:"mode"`
	HTTPProxy        string `json:"httpProxy"`
	HTTPSProxy       string `json:"httpsProxy"`
	NoProxy          string `json:"noProxy"`
	Username         string `json:"username"`
	Password         string `json:"password,omitempty"`
	ClearCredentials bool   `json:"clearCredentials"`
	CAPEM            string `json:"caPEM,omitempty"`
	ClearCA          bool   `json:"clearCA"`
	Revision         string `json:"revision,omitempty"`
}

// View is safe to return to the browser: it contains no password or CA PEM.
type View struct {
	Mode                  string              `json:"mode"`
	EffectiveMode         string              `json:"effectiveMode"`
	Enabled               bool                `json:"enabled"`
	HTTPProxy             string              `json:"httpProxy,omitempty"`
	HTTPSProxy            string              `json:"httpsProxy,omitempty"`
	NoProxy               string              `json:"noProxy,omitempty"`
	Username              string              `json:"username,omitempty"`
	CredentialsConfigured bool                `json:"credentialsConfigured"`
	CAConfigured          bool                `json:"caConfigured"`
	Source                string              `json:"source"`
	Revision              string              `json:"revision,omitempty"`
	UpdatedAt             time.Time           `json:"updatedAt,omitempty"`
	NetworkPolicy         NetworkPolicyStatus `json:"networkPolicy"`
}

type Controller struct {
	factory      *Factory
	store        Store
	policy       NetworkPolicyManager
	pollInterval time.Duration
	logger       *slog.Logger

	mu         sync.Mutex
	revision   string
	storedMode string
	updatedAt  time.Time
}

func NewController(factory *Factory, store Store, logger *slog.Logger) *Controller {
	return &Controller{factory: factory, store: store, pollInterval: defaultPollInterval, logger: logger}
}

func (c *Controller) SetNetworkPolicyManager(manager NetworkPolicyManager) {
	if c != nil {
		c.policy = manager
	}
}

func (c *Controller) SetPollInterval(interval time.Duration) {
	if interval > 0 {
		c.pollInterval = interval
	}
}

// Load applies the persisted override. A missing data file means the startup
// configuration remains active, preserving upgrades from releases that predate
// runtime egress settings.
func (c *Controller) Load(ctx context.Context) error {
	if c == nil || c.store == nil {
		return nil
	}
	stored, found, err := c.store.LoadEgressSettings(ctx)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !found && c.revision == "" {
		return nil
	}
	if found && stored.Revision == c.revision && stored.Mode == c.storedMode {
		return nil
	}
	if !found || stored.Mode == "bootstrap" {
		if c.policy != nil {
			if err := c.policy.Cleanup(ctx); err != nil {
				return err
			}
		}
		c.factory.Reset()
	} else {
		settings := Settings{
			Mode: stored.Mode, HTTPProxy: stored.HTTPProxy, HTTPSProxy: stored.HTTPSProxy,
			NoProxy: stored.NoProxy, Username: stored.Username, Password: stored.Password, CAPEM: stored.CAPEM,
		}
		if c.policy != nil {
			targets, resolveErr := resolveProxyTargets(ctx, settings)
			if resolveErr != nil {
				return resolveErr
			}
			if err := c.policy.Ensure(ctx, targets); err != nil {
				return err
			}
		}
		if err := c.factory.Update(settings); err != nil {
			return err
		}
	}
	if found {
		c.revision = stored.Revision
		c.storedMode = stored.Mode
		c.updatedAt = stored.UpdatedAt
	} else {
		c.revision = ""
		c.storedMode = ""
		c.updatedAt = time.Time{}
	}
	return nil
}

func (c *Controller) Run(ctx context.Context) {
	if c == nil || c.store == nil {
		return
	}
	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.Load(ctx); err != nil && c.logger != nil {
				c.logger.Warn("egress_runtime_reload_failed", "error", err)
			}
		}
	}
}

func (c *Controller) View() View {
	if c == nil || c.factory == nil {
		return View{Mode: "bootstrap", EffectiveMode: "direct", Source: SourceBootstrap}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	status := c.factory.Status()
	mode := "bootstrap"
	if status.Source == SourceRuntime {
		mode = status.Mode
	}
	current := c.factory.Current()
	return View{
		Mode: mode, EffectiveMode: status.Mode, Enabled: status.Enabled,
		HTTPProxy: status.HTTPProxy, HTTPSProxy: status.HTTPSProxy, NoProxy: status.NoProxy,
		Username: current.Username, CredentialsConfigured: status.CredentialsConfigured,
		CAConfigured: status.CAConfigured, Source: status.Source,
		Revision: c.revision, UpdatedAt: c.updatedAt, NetworkPolicy: c.networkPolicyStatus(),
	}
}

func (c *Controller) Status() Status {
	if c == nil || c.factory == nil {
		return Status{Mode: "direct", Source: SourceBootstrap}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.factory.Status()
}

func (c *Controller) Save(ctx context.Context, input UpdateInput) (View, error) {
	if c == nil || c.store == nil || c.factory == nil {
		return View{}, fmt.Errorf("%w: runtime egress settings are unavailable", domain.ErrUnsupported)
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	mode := strings.ToLower(strings.TrimSpace(input.Mode))
	stored := StoredSettings{Mode: mode}
	var settings Settings
	switch mode {
	case "bootstrap":
		// Persist a tombstone so other replicas also return to startup settings.
		settings = Settings{Mode: "bootstrap"}
	case "direct":
		settings = Settings{Mode: "direct"}
		if _, err := buildState(settings, SourceRuntime, 0); err != nil {
			return View{}, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
		}
	case "explicit":
		current := c.factory.Current()
		username, password := strings.TrimSpace(input.Username), input.Password
		if input.ClearCredentials {
			username, password = "", ""
		} else if password == "" && current.Password != "" && username == current.Username {
			password = current.Password
		}
		if (username == "") != (password == "") {
			return View{}, fmt.Errorf("%w: proxy username and password must be configured together", domain.ErrInvalid)
		}
		caPEM := current.CAPEM
		if input.ClearCA {
			caPEM = nil
		} else if strings.TrimSpace(input.CAPEM) != "" {
			caPEM = []byte(strings.TrimSpace(input.CAPEM))
		}
		settings = Settings{
			Mode: "explicit", HTTPProxy: strings.TrimSpace(input.HTTPProxy), HTTPSProxy: strings.TrimSpace(input.HTTPSProxy),
			NoProxy: strings.TrimSpace(input.NoProxy), Username: username, Password: password, CAPEM: caPEM,
		}
		if _, err := buildState(settings, SourceRuntime, 0); err != nil {
			return View{}, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
		}
		stored.HTTPProxy = settings.HTTPProxy
		stored.HTTPSProxy = settings.HTTPSProxy
		stored.NoProxy = settings.NoProxy
		stored.Username = settings.Username
		stored.Password = settings.Password
		stored.CAPEM = settings.CAPEM
	default:
		return View{}, fmt.Errorf("%w: mode must be bootstrap, explicit, or direct", domain.ErrInvalid)
	}

	preflight, targets, err := c.preflightSettings(ctx, settings)
	if err != nil {
		return View{}, err
	}
	if preflight.Status == "error" {
		return View{}, &domain.ValidationError{Field: "networkPolicy", Message: preflight.Message}
	}

	previous := c.factory.Current()
	if mode == "explicit" && c.policy != nil {
		if err := c.policy.Ensure(ctx, targets); err != nil {
			return View{}, err
		}
	}

	saved, err := c.store.SaveEgressSettings(ctx, stored, input.Revision)
	if err != nil {
		c.rollbackNetworkPolicy(ctx, previous)
		return View{}, err
	}
	if saved.Mode == "bootstrap" {
		c.factory.Reset()
	} else if err := c.factory.Update(Settings{
		Mode: saved.Mode, HTTPProxy: saved.HTTPProxy, HTTPSProxy: saved.HTTPSProxy,
		NoProxy: saved.NoProxy, Username: saved.Username, Password: saved.Password, CAPEM: saved.CAPEM,
	}); err != nil {
		return View{}, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}
	if c.policy != nil && saved.Mode != "explicit" {
		if err := c.policy.Cleanup(ctx); err != nil {
			return View{}, err
		}
	}
	c.revision = saved.Revision
	c.storedMode = saved.Mode
	c.updatedAt = saved.UpdatedAt
	return c.viewLocked(), nil
}

func (c *Controller) viewLocked() View {
	status := c.factory.Status()
	mode := "bootstrap"
	if status.Source == SourceRuntime {
		mode = status.Mode
	}
	current := c.factory.Current()
	return View{
		Mode: mode, EffectiveMode: status.Mode, Enabled: status.Enabled,
		HTTPProxy: status.HTTPProxy, HTTPSProxy: status.HTTPSProxy, NoProxy: status.NoProxy,
		Username: current.Username, CredentialsConfigured: status.CredentialsConfigured,
		CAConfigured: status.CAConfigured, Source: status.Source,
		Revision: c.revision, UpdatedAt: c.updatedAt, NetworkPolicy: c.networkPolicyStatus(),
	}
}

func (c *Controller) rollbackNetworkPolicy(ctx context.Context, previous Settings) {
	if c == nil || c.policy == nil {
		return
	}
	if strings.EqualFold(strings.TrimSpace(previous.Mode), "explicit") {
		if targets, err := resolveProxyTargets(ctx, previous); err == nil {
			if err := c.policy.Ensure(ctx, targets); err == nil {
				return
			}
		}
	}
	if err := c.policy.Cleanup(ctx); err != nil && c.logger != nil {
		c.logger.Warn("egress_network_policy_rollback_failed", "error", err)
	}
}

func (c *Controller) networkPolicyStatus() NetworkPolicyStatus {
	if c == nil || c.policy == nil {
		return NetworkPolicyStatus{Mode: NetworkPolicyModeDisabled, Message: "NetworkPolicy management is unavailable"}
	}
	return c.policy.Status()
}

func IsConflict(err error) bool {
	return errors.Is(err, domain.ErrConflict)
}
