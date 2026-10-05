package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/go-ldap/ldap/v3"

	"github.com/horizon/orion/sdk/go/security"
)

var (
	_ldapURL      = os.Getenv("LDAP_URL")
	_ldapBaseDN   = os.Getenv("LDAP_BASE_DN")
	_ldapBindDN   = os.Getenv("LDAP_BIND_DN")
	_ldapBindPW   = os.Getenv("LDAP_BIND_PASSWORD")
	_ldapUserAttr = getEnvOr("LDAP_USER_ATTR", "uid")
	_ldapGroupAttr = getEnvOr("LDAP_GROUP_ATTR", "memberOf")
	_idGenerator   = security.NewIDGenerator("ldap")
)

type User struct {
	DN         string            `json:"dn"`
	UID        string            `json:"uid"`
	CN         string            `json:"cn"`
	Email      string            `json:"email"`
	Groups     []string          `json:"groups"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

type Group struct {
	DN   string   `json:"dn"`
	CN   string   `json:"cn"`
	GID  string   `json:"gid"`
	Members []string `json:"members"`
}

type AuthRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type AuthResponse struct {
	Success bool   `json:"success"`
	User    *User  `json:"user,omitempty"`
	Error   string `json:"error,omitempty"`
}

func main() {
	if _ldapURL == "" {
		log.Fatal("LDAP_URL is required")
	}
	if _ldapBaseDN == "" {
		log.Fatal("LDAP_BASE_DN is required")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/auth", handleAuth)
	mux.HandleFunc("/user/", handleUser)
	mux.HandleFunc("/users", handleUsers)
	mux.HandleFunc("/groups", handleGroups)
	mux.HandleFunc("/group/", handleGroup)

	port := getEnvOr("PORT", "8080")
	log.Printf("identity-ldap starting on :%s", port)
	log.Printf("LDAP: %s, BaseDN: %s", _ldapURL, _ldapBaseDN)

	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func getLDAPConnection() (*ldap.Conn, error) {
	conn, err := ldap.DialURL(_ldapURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to LDAP: %w", err)
	}

	if _ldapBindDN != "" && _ldapBindPW != "" {
		if err := conn.Bind(_ldapBindDN, _ldapBindPW); err != nil {
			conn.Close()
			return nil, fmt.Errorf("LDAP bind failed: %w", err)
		}
	}

	return conn, nil
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	conn, err := getLDAPConnection()
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"status": "unhealthy", "error": err.Error()})
		return
	}
	defer conn.Close()

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "healthy",
		"url":     _ldapURL,
		"base_dn": _ldapBaseDN,
	})
}

func handleAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req AuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	conn, err := getLDAPConnection()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	userDN, err := findUserDN(conn, req.Username)
	if err != nil {
		http.Error(w, fmt.Sprintf("User not found: %v", err), http.StatusUnauthorized)
		return
	}

	if err := conn.Bind(userDN, req.Password); err != nil {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	user, err := getUserByDN(conn, userDN)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get user: %v", err), http.StatusInternalServerError)
		return
	}

	groups, _ := getUserGroups(conn, userDN)
	user.Groups = groups

	json.NewEncoder(w).Encode(AuthResponse{
		Success: true,
		User:    user,
	})
}

func handleUser(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/user/")
	if path == "" {
		http.Error(w, "User DN required", http.StatusBadRequest)
		return
	}

	conn, err := getLDAPConnection()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	switch r.Method {
	case http.MethodGet:
		user, err := getUserByDN(conn, path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		groups, _ := getUserGroups(conn, user.DN)
		user.Groups = groups
		json.NewEncoder(w).Encode(user)

	case http.MethodDelete:
		if err := deleteUser(conn, path); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleUsers(w http.ResponseWriter, r *http.Request) {
	conn, err := getLDAPConnection()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	searchRequest := ldap.NewSearchRequest(
		_ldapBaseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
		fmt.Sprintf("(%s=*)", _ldapUserAttr),
		[]string{"dn", _ldapUserAttr, "cn", "mail", _ldapGroupAttr},
		nil,
	)

	result, err := conn.Search(searchRequest)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	users := make([]*User, 0, len(result.Entries))
	for _, entry := range result.Entries {
		user := &User{
			DN:         entry.DN,
			UID:        entry.GetAttributeValue(_ldapUserAttr),
			CN:         entry.GetAttributeValue("cn"),
			Email:      entry.GetAttributeValue("mail"),
			Attributes: entryAttributes(entry),
		}
		users = append(users, user)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"items": users,
		"total": len(users),
	})
}

func handleGroups(w http.ResponseWriter, r *http.Request) {
	conn, err := getLDAPConnection()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	searchRequest := ldap.NewSearchRequest(
		_ldapBaseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
		"(objectClass=groupOfNames)",
		[]string{"dn", "cn", "gidNumber", "member"},
		nil,
	)

	result, err := conn.Search(searchRequest)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	groups := make([]*Group, 0, len(result.Entries))
	for _, entry := range result.Entries {
		group := &Group{
			DN:   entry.DN,
			CN:   entry.GetAttributeValue("cn"),
			GID:  entry.GetAttributeValue("gidNumber"),
		}
		for _, m := range entry.GetAttributeValues("member") {
			group.Members = append(group.Members, m)
		}
		groups = append(groups, group)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"items": groups,
		"total": len(groups),
	})
}

func handleGroup(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/group/")
	if path == "" {
		http.Error(w, "Group DN required", http.StatusBadRequest)
		return
	}

	conn, err := getLDAPConnection()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	group, err := getGroupByDN(conn, path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	json.NewEncoder(w).Encode(group)
}

func findUserDN(conn *ldap.Conn, username string) (string, error) {
	searchRequest := ldap.NewSearchRequest(
		_ldapBaseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 1, 0, false,
		fmt.Sprintf("(%s=%s)", _ldapUserAttr, ldap.EscapeFilter(username)),
		[]string{"dn"},
		nil,
	)

	result, err := conn.Search(searchRequest)
	if err != nil {
		return "", err
	}

	if len(result.Entries) == 0 {
		return "", fmt.Errorf("user not found: %s", username)
	}

	return result.Entries[0].DN, nil
}

func getUserByDN(conn *ldap.Conn, dn string) (*User, error) {
	searchRequest := ldap.NewSearchRequest(
		dn,
		ldap.ScopeBaseObject, ldap.NeverDerefAliases, 1, 0, false,
		"(objectClass=*)",
		[]string{"dn", _ldapUserAttr, "cn", "mail", "givenName", "sn", _ldapGroupAttr},
		nil,
	)

	result, err := conn.Search(searchRequest)
	if err != nil {
		return nil, err
	}

	if len(result.Entries) == 0 {
		return nil, fmt.Errorf("user not found")
	}

	entry := result.Entries[0]
	return &User{
		DN:         entry.DN,
		UID:        entry.GetAttributeValue(_ldapUserAttr),
		CN:         entry.GetAttributeValue("cn"),
		Email:      entry.GetAttributeValue("mail"),
		Attributes: entryAttributes(entry),
	}, nil
}

func entryAttributes(entry *ldap.Entry) map[string]string {
	attributes := make(map[string]string, len(entry.Attributes))
	for _, attribute := range entry.Attributes {
		if len(attribute.Values) > 0 {
			attributes[attribute.Name] = attribute.Values[0]
		}
	}
	return attributes
}

func getUserGroups(conn *ldap.Conn, userDN string) ([]string, error) {
	searchRequest := ldap.NewSearchRequest(
		_ldapBaseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
		fmt.Sprintf("(&(%s=%s)(objectClass=groupOfNames))", _ldapGroupAttr, ldap.EscapeFilter(userDN)),
		[]string{"cn"},
		nil,
	)

	result, err := conn.Search(searchRequest)
	if err != nil {
		return nil, err
	}

	groups := make([]string, 0, len(result.Entries))
	for _, entry := range result.Entries {
		groups = append(groups, entry.GetAttributeValue("cn"))
	}

	return groups, nil
}

func getGroupByDN(conn *ldap.Conn, dn string) (*Group, error) {
	searchRequest := ldap.NewSearchRequest(
		dn,
		ldap.ScopeBaseObject, ldap.NeverDerefAliases, 1, 0, false,
		"(objectClass=*)",
		[]string{"dn", "cn", "gidNumber", "member"},
		nil,
	)

	result, err := conn.Search(searchRequest)
	if err != nil {
		return nil, err
	}

	if len(result.Entries) == 0 {
		return nil, fmt.Errorf("group not found")
	}

	entry := result.Entries[0]
	group := &Group{
		DN:   entry.DN,
		CN:   entry.GetAttributeValue("cn"),
		GID:  entry.GetAttributeValue("gidNumber"),
	}
	for _, m := range entry.GetAttributeValues("member") {
		group.Members = append(group.Members, m)
	}

	return group, nil
}

func deleteUser(conn *ldap.Conn, dn string) error {
	delRequest := ldap.NewDelRequest(dn, nil)
	return conn.Del(delRequest)
}

func getEnvOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
