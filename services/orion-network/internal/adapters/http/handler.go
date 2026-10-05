package httpadapter

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/horizon/orion/libs/go/kit/apierror"
	"github.com/horizon/orion/libs/go/kit/httpx"
	networkkit "github.com/horizon/orion/libs/go/kit/network"
	"github.com/horizon/orion/services/orion-network/internal/application"
)

type Handler struct {
	logger  *slog.Logger
	service *application.Service
}

func NewHandler(logger *slog.Logger, service *application.Service) *Handler {
	return &Handler{logger: logger, service: service}
}

func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(httpx.RequestID)

	r.Get("/healthz", h.handleHealth)
	r.Get("/readyz", h.handleReady)

	r.Route("/v1/networks", func(r chi.Router) {
		r.Get("/", h.handleListNetworks)
		r.Post("/", h.handleCreateNetwork)
		r.Get("/{networkID}", h.handleGetNetwork)
		r.Delete("/{networkID}", h.handleDeleteNetwork)
	})

	r.Route("/v1/subnets", func(r chi.Router) {
		r.Get("/", h.handleListSubnets)
		r.Post("/", h.handleCreateSubnet)
		r.Get("/{subnetID}", h.handleGetSubnet)
	})

	r.Route("/v1/ports", func(r chi.Router) {
		r.Get("/", h.handleListPorts)
		r.Post("/", h.handleCreatePort)
		r.Get("/{portID}", h.handleGetPort)
		r.Delete("/{portID}", h.handleDeletePort)
		r.Post("/{portID}/binding", h.handlePortBinding)
	})

	r.Route("/v1/security-groups", func(r chi.Router) {
		r.Get("/", h.handleListSecurityGroups)
		r.Post("/", h.handleCreateSecurityGroup)
		r.Get("/{groupID}", h.handleGetSecurityGroup)
		r.Delete("/{groupID}", h.handleDeleteSecurityGroup)
		r.Get("/{groupID}/rules", h.handleListSecurityGroupRules)
		r.Post("/{groupID}/rules", h.handleCreateSecurityGroupRule)
	})
	r.Delete("/v1/security-group-rules/{ruleID}", h.handleDeleteSecurityGroupRule)

	r.Route("/v1/routers", func(r chi.Router) {
		r.Get("/", h.handleListRouters)
		r.Post("/", h.handleCreateRouter)
		r.Get("/{routerID}", h.handleGetRouter)
		r.Delete("/{routerID}", h.handleDeleteRouter)
		r.Get("/{routerID}/interfaces", h.handleListRouterInterfaces)
		r.Post("/{routerID}/interfaces", h.handleCreateRouterInterface)
	})
	r.Delete("/v1/router-interfaces/{interfaceID}", h.handleDeleteRouterInterface)
	r.Route("/v1/floating-ips", func(r chi.Router) {
		r.Get("/", h.handleListFloatingIPs)
		r.Post("/", h.handleCreateFloatingIP)
		r.Get("/{floatingIPID}", h.handleGetFloatingIP)
		r.Delete("/{floatingIPID}", h.handleDeleteFloatingIP)
	})

	return r
}

func (h *Handler) handleListRouters(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.ListRouters(r.Context(), r.URL.Query().Get("project_id"))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string][]networkkit.Router{"routers": items})
}
func (h *Handler) handleCreateRouter(w http.ResponseWriter, r *http.Request) {
	var req networkkit.CreateRouterRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		apierror.Write(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	item, err := h.service.CreateRouter(r.Context(), req)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]networkkit.Router{"router": item})
}
func (h *Handler) handleGetRouter(w http.ResponseWriter, r *http.Request) {
	item, err := h.service.GetRouter(r.Context(), chi.URLParam(r, "routerID"))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]networkkit.Router{"router": item})
}
func (h *Handler) handleDeleteRouter(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteRouter(r.Context(), chi.URLParam(r, "routerID")); err != nil {
		h.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h *Handler) handleListRouterInterfaces(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.ListRouterInterfaces(r.Context(), chi.URLParam(r, "routerID"))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string][]networkkit.RouterInterface{"interfaces": items})
}
func (h *Handler) handleCreateRouterInterface(w http.ResponseWriter, r *http.Request) {
	var req networkkit.CreateRouterInterfaceRequest
	req.RouterID = chi.URLParam(r, "routerID")
	if err := httpx.DecodeJSON(r, &req); err != nil {
		apierror.Write(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	item, err := h.service.CreateRouterInterface(r.Context(), req)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]networkkit.RouterInterface{"interface": item})
}
func (h *Handler) handleDeleteRouterInterface(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteRouterInterface(r.Context(), chi.URLParam(r, "interfaceID")); err != nil {
		h.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (h *Handler) handleListFloatingIPs(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.ListFloatingIPs(r.Context(), r.URL.Query().Get("project_id"))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string][]networkkit.FloatingIP{"floating_ips": items})
}
func (h *Handler) handleCreateFloatingIP(w http.ResponseWriter, r *http.Request) {
	var req networkkit.CreateFloatingIPRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		apierror.Write(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	item, err := h.service.CreateFloatingIP(r.Context(), req)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]networkkit.FloatingIP{"floating_ip": item})
}
func (h *Handler) handleGetFloatingIP(w http.ResponseWriter, r *http.Request) {
	item, err := h.service.GetFloatingIP(r.Context(), chi.URLParam(r, "floatingIPID"))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]networkkit.FloatingIP{"floating_ip": item})
}
func (h *Handler) handleDeleteFloatingIP(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteFloatingIP(r.Context(), chi.URLParam(r, "floatingIPID")); err != nil {
		h.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleReady(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (h *Handler) handleListNetworks(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string][]networkkit.Network{
		"networks": h.service.ListNetworks(r.Context()),
	})
}

func (h *Handler) handleCreateNetwork(w http.ResponseWriter, r *http.Request) {
	var req networkkit.CreateNetworkRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		apierror.Write(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	item, err := h.service.CreateNetwork(r.Context(), req)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]networkkit.Network{"network": item})
}

func (h *Handler) handleGetNetwork(w http.ResponseWriter, r *http.Request) {
	networkID := chi.URLParam(r, "networkID")
	item, err := h.service.GetNetwork(r.Context(), networkID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]networkkit.Network{"network": item})
}

func (h *Handler) handleDeleteNetwork(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteNetwork(r.Context(), chi.URLParam(r, "networkID")); err != nil {
		h.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleListSubnets(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string][]networkkit.Subnet{
		"subnets": h.service.ListSubnets(r.Context()),
	})
}

func (h *Handler) handleCreateSubnet(w http.ResponseWriter, r *http.Request) {
	var req networkkit.CreateSubnetRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		apierror.Write(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	item, err := h.service.CreateSubnet(r.Context(), req)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]networkkit.Subnet{"subnet": item})
}

func (h *Handler) handleGetSubnet(w http.ResponseWriter, r *http.Request) {
	subnetID := chi.URLParam(r, "subnetID")
	item, err := h.service.GetSubnet(r.Context(), subnetID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]networkkit.Subnet{"subnet": item})
}

func (h *Handler) handleListPorts(w http.ResponseWriter, r *http.Request) {
	bindingHostID := r.URL.Query().Get("binding_host_id")
	httpx.WriteJSON(w, http.StatusOK, map[string][]networkkit.Port{
		"ports": h.service.ListPorts(r.Context(), bindingHostID),
	})
}

func (h *Handler) handleCreatePort(w http.ResponseWriter, r *http.Request) {
	var req networkkit.CreatePortRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		apierror.Write(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	item, err := h.service.CreatePort(r.Context(), req)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]networkkit.Port{"port": item})
}

func (h *Handler) handleGetPort(w http.ResponseWriter, r *http.Request) {
	portID := chi.URLParam(r, "portID")
	item, err := h.service.GetPort(r.Context(), portID)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]networkkit.Port{"port": item})
}

func (h *Handler) handleDeletePort(w http.ResponseWriter, r *http.Request) {
	portID := chi.URLParam(r, "portID")
	if err := h.service.DeletePort(r.Context(), portID); err != nil {
		h.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handlePortBinding(w http.ResponseWriter, r *http.Request) {
	portID := chi.URLParam(r, "portID")
	var req networkkit.UpdatePortBindingRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		apierror.Write(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	item, err := h.service.UpdatePortBinding(r.Context(), portID, req)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]networkkit.Port{"port": item})
}

func (h *Handler) handleListSecurityGroups(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.ListSecurityGroups(r.Context(), r.URL.Query().Get("project_id"))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string][]networkkit.SecurityGroup{"security_groups": items})
}

func (h *Handler) handleCreateSecurityGroup(w http.ResponseWriter, r *http.Request) {
	var req networkkit.CreateSecurityGroupRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		apierror.Write(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	item, err := h.service.CreateSecurityGroup(r.Context(), req)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]networkkit.SecurityGroup{"security_group": item})
}

func (h *Handler) handleGetSecurityGroup(w http.ResponseWriter, r *http.Request) {
	item, err := h.service.GetSecurityGroup(r.Context(), chi.URLParam(r, "groupID"))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]networkkit.SecurityGroup{"security_group": item})
}

func (h *Handler) handleDeleteSecurityGroup(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteSecurityGroup(r.Context(), chi.URLParam(r, "groupID")); err != nil {
		h.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleListSecurityGroupRules(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.ListSecurityGroupRules(r.Context(), chi.URLParam(r, "groupID"))
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string][]networkkit.SecurityGroupRule{"security_group_rules": items})
}

func (h *Handler) handleCreateSecurityGroupRule(w http.ResponseWriter, r *http.Request) {
	var req networkkit.CreateSecurityGroupRuleRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		apierror.Write(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	req.SecurityGroupID = chi.URLParam(r, "groupID")
	item, err := h.service.CreateSecurityGroupRule(r.Context(), req)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]networkkit.SecurityGroupRule{"security_group_rule": item})
}

func (h *Handler) handleDeleteSecurityGroupRule(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteSecurityGroupRule(r.Context(), chi.URLParam(r, "ruleID")); err != nil {
		h.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, application.ErrBackendUnavailable):
		apierror.Write(w, http.StatusServiceUnavailable, "backend_unavailable", "OVN/OVS backend unavailable")
	case errors.Is(err, application.ErrNetworkNotFound):
		apierror.Write(w, http.StatusNotFound, "network_not_found", "network not found")
	case errors.Is(err, application.ErrNetworkInUse):
		apierror.Write(w, http.StatusConflict, "network_in_use", "network has dependent resources")
	case errors.Is(err, application.ErrSubnetNotFound):
		apierror.Write(w, http.StatusNotFound, "subnet_not_found", "subnet not found")
	case errors.Is(err, application.ErrPortNotFound):
		apierror.Write(w, http.StatusNotFound, "port_not_found", "port not found")
	case errors.Is(err, application.ErrBindingHostMismatch):
		apierror.Write(w, http.StatusConflict, "binding_host_mismatch", "binding host does not match port owner")
	case errors.Is(err, application.ErrNoSubnetOnNet):
		apierror.Write(w, http.StatusConflict, "no_subnet_on_network", "network has no subnet")
	case errors.Is(err, application.ErrInvalidCIDR):
		apierror.Write(w, http.StatusBadRequest, "invalid_cidr", "invalid CIDR")
	case errors.Is(err, application.ErrQuotaExceeded):
		apierror.Write(w, http.StatusTooManyRequests, "quota_exceeded", "project quota exceeded")
	case errors.Is(err, application.ErrSecurityGroupNotFound):
		apierror.Write(w, http.StatusNotFound, "security_group_not_found", "security group not found")
	case errors.Is(err, application.ErrSecurityGroupRuleNotFound):
		apierror.Write(w, http.StatusNotFound, "security_group_rule_not_found", "security group rule not found")
	case errors.Is(err, application.ErrInvalidSecurityGroupRule):
		apierror.Write(w, http.StatusBadRequest, "invalid_security_group_rule", "invalid security group rule")
	case errors.Is(err, application.ErrRouterNotFound), errors.Is(err, application.ErrRouterInterfaceNotFound), errors.Is(err, application.ErrFloatingIPNotFound):
		apierror.Write(w, http.StatusNotFound, "router_resource_not_found", "router resource not found")
	case errors.Is(err, application.ErrRouterInUse):
		apierror.Write(w, http.StatusConflict, "router_in_use", "router has dependent resources")
	case errors.Is(err, application.ErrInvalidRouterRequest):
		apierror.Write(w, http.StatusBadRequest, "invalid_router_request", "invalid router request")
	case errors.Is(err, application.ErrRouterBackendUnsupported):
		apierror.Write(w, http.StatusNotImplemented, "router_backend_unsupported", "router backend is not supported")
	default:
		h.logger.Error("network request failed", "error", err.Error())
		apierror.Write(w, http.StatusInternalServerError, "internal_error", "internal error")
	}
}
