package ui

// Column and field labels used by `vertra template` and `vertra stack`. They
// are merged into the shared table at start-up so the main table in render.go
// stays as it is.
func init() {
	extra := map[string]map[string]string{
		"pt": {
			"slug": "Slug", "category": "Categoria", "origin": "Origem", "members": "Membros", "template": "Template",
			"count": "Total", "security": "Segurança", "link_token": "Token do link", "max_uses": "Usos máximos", "expires_at": "Expira em",
			"revoked_at": "Revogado em", "uses": "Usos", "last4": "Final", "mode": "Modo", "location": "Local", "code": "Código",
			"required_plan": "Plano exigido", "valid": "Válido", "services": "Serviços", "ram_min_total": "Memória mínima",
		},
		"es": {
			"slug": "Slug", "category": "Categoría", "origin": "Origen", "members": "Miembros", "template": "Plantilla",
			"count": "Total", "security": "Seguridad", "link_token": "Token del enlace", "max_uses": "Usos máximos", "expires_at": "Expira",
			"revoked_at": "Revocado", "uses": "Usos", "last4": "Final", "mode": "Modo", "location": "Lugar", "code": "Código",
			"required_plan": "Plan requerido", "valid": "Válido", "services": "Servicios", "ram_min_total": "Memoria mínima",
		},
		"en": {
			"slug": "Slug", "category": "Category", "origin": "Origin", "members": "Members", "template": "Template",
			"count": "Count", "security": "Security", "link_token": "Link token", "max_uses": "Max uses", "expires_at": "Expires",
			"revoked_at": "Revoked", "uses": "Uses", "last4": "Last 4", "mode": "Mode", "location": "Location", "code": "Code",
			"required_plan": "Required plan", "valid": "Valid", "services": "Services", "ram_min_total": "Minimum memory",
		},
	}
	for locale, m := range extra {
		for k, v := range m {
			if translations[locale][k] == "" {
				translations[locale][k] = v
			}
		}
	}
}
