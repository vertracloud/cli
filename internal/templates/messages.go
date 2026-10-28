package templates

import (
	"errors"
	"fmt"
	"strings"

	"github.com/vertracloud/cli/internal/ui"
	"github.com/vertracloud/sdk-api-go/rest"
	"github.com/vertracloud/sdk-api-go/vertracloud"
)

// Messages is the text of the template and stack commands, by language.
// Keys that are API error codes replace the API message of that error.
var messages = map[string]map[string]string{
	"pt": {
		"spin_pack":        "Empacotando o código…",
		"spin_upload":      "Enviando o template…",
		"spin_validate":    "Validando o manifesto…",
		"manifest_ok":      "Manifesto válido.",
		"manifest_bad":     "Manifesto inválido: %d problema(s).",
		"yaml_syntax":      "YAML inválido",
		"deploy_started":   "Criando %s…",
		"deploy_done":      "Concluído.",
		"deploy_failed":    "O deploy falhou e foi desfeito.",
		"deploy_still":     "O deploy continua em andamento. Acompanhe com o painel; id %s.",
		"outputs":          "Saídas",
		"secret_once":      "Este valor aparece só agora e não poderá ser lido de novo. Guarde-o.",
		"output_read":      "(já lido; não é mostrado de novo)",
		"created":          "Criado",
		"resources":        "Recursos criados",
		"ask_name":         "Nome do app ou da stack",
		"ask_yes_no":       "s/n",
		"ask_choose":       "Escolha (número ou valor)",
		"ask_required":     "Resposta obrigatória.",
		"ask_invalid":      "Resposta inválida: %s",
		"missing_answers":  "Faltam respostas (use --set chave=valor ou rode em um terminal): %s",
		"unknown_inputs":   "Pergunta desconhecida em --set: %s. Perguntas deste template: %s",
		"bad_set":          "Use --set chave=valor (recebi %q).",
		"bad_service":      "Use --service <serviço>.<name|ram|subdomain>=<valor> (recebi %q).",
		"unknown_service":  "Serviço desconhecido em --service: %s. Serviços: %s",
		"name_required":    "Informe --name.",
		"need_template":    "Informe o slug do template, ou --share com o link.",
		"v_not_number":     "não é um número",
		"v_not_bool":       "use sim/não (true/false)",
		"v_not_option":     "valores aceitos: %s",
		"v_min_length":     "mínimo de %d caracteres",
		"v_max_length":     "máximo de %d caracteres",
		"v_min":            "mínimo %v",
		"v_max":            "máximo %v",
		"v_regex":          "formato inválido",
		"share_once":       "O token do link aparece só agora. Guarde-o; quem o tiver pode usar o template.",
		"share_mode_bad":   "--mode deve ser use ou full_copy.",
		"share_revoked":    "Link revogado.",
		"pack_too_large":   "O código do serviço passa de 100 MB. Nada foi enviado; use um .vertraignore para tirar arquivos.",
		"secret_in_source": "O código enviado tem arquivos com segredos; tire-os (ou ponha no .vertraignore): %s",
		"changelog_hint":   "Dica: use --changelog para descrever a versão.",
		"created_hint":     "Use com: vertra template deploy %s --name <nome>",
		"newver_hint":      "Versão %s publicada.",

		"TEMPLATE_NOT_FOUND":              "Template não encontrado.",
		"TEMPLATE_VERSION_NOT_FOUND":      "Versão do template não encontrada.",
		"TEMPLATE_SLUG_TAKEN":             "Já existe um template seu com este id (slug).",
		"TEMPLATE_INPUT_INVALID":          "Alguma resposta é inválida",
		"TEMPLATE_SOURCE_REQUIRED":        "Falta o código de um serviço do tipo code.",
		"TEMPLATE_PLAN_REQUIRED":          "Seu plano não permite usar este template.",
		"TEMPLATE_RAM_INSUFFICIENT":       "Memória insuficiente na sua conta para este template.",
		"TEMPLATE_IMAGE_SLOTS_EXHAUSTED":  "Suas vagas de imagem acabaram.",
		"TEMPLATE_CLOSED":                 "Este template é fechado: o código não é acessível.",
		"TEMPLATE_PRIVATE_LIMIT_REACHED":  "Você chegou ao limite de templates privados.",
		"TEMPLATE_DEPLOY_IN_PROGRESS":     "Já há um deploy deste template em andamento.",
		"TEMPLATE_DEPLOY_FAILED":          "O deploy falhou e foi desfeito.",
		"TEMPLATE_DEPLOY_NOT_FOUND":       "Deploy não encontrado.",
		"IDEMPOTENCY_KEY_CONFLICT":        "Esta chave de idempotência já foi usada com outro pedido.",
		"TEMPLATE_UPDATE_BLOCKED":         "A atualização não pode ser feita agora",
		"TEMPLATE_DEPRECATED":             "Este template foi descontinuado.",
		"TEMPLATE_VERSION_NOT_SELECTABLE": "Este template não permite escolher a versão.",
		"TEMPLATE_STORAGE_UNAVAILABLE":    "O armazenamento de templates está indisponível. Tente de novo em instantes.",
		"SHARE_LINK_NOT_FOUND":            "Link não encontrado.",
		"SHARE_LINK_REVOKED":              "Este link foi revogado.",
		"SHARE_LINK_EXPIRED":              "Este link expirou.",
		"SHARE_LINK_EXHAUSTED":            "Este link já foi usado o máximo de vezes.",
		"STACK_NOT_FOUND":                 "Stack não encontrada.",
		"STACK_NAME_MISMATCH":             "O nome digitado não é o nome da stack.",
		"STACK_NETWORK_MODE_UNAVAILABLE":  "Esse modo de rede ainda não está disponível.",
		"APP_BUILDING":                    "Um app da stack ainda está sendo construído. Espere terminar.",
		"stack_confirm_required":          "Confirmação necessária para excluir a stack %q: rode de novo com --yes.",
		"stack_confirm_warn_all":          "Excluir a stack %q também exclui os %d membros (apps, bancos e Blob conforme --blob).",
		"stack_confirm_warn_dissolve":     "Excluir a stack %q desfaz só o grupo: os %d membros continuam como recursos soltos.",
		"stack_confirm_prompt":            "Digite o nome da stack (%s) para confirmar: ",
		"stack_blob_needs_all":            "--blob só vale com --delete-all (sem ele, os arquivos do Blob são mantidos).",
		"stack_blob_bad":                  "--blob deve ser keep ou delete.",
		"stack_update_none":               "A stack já está na última versão do template (%s).",
		"stack_update_no_template":        "Esta stack não veio de um template.",
		"stack_update_blocked":            "Atualização bloqueada: %s.",
		"stack_update_available":          "Atualização disponível: %s → %s.",
		"stack_update_security":           "Esta versão corrige um problema de segurança.",
		"stack_update_permissions":        "Esta versão concede mais permissões ao template.",
		"stack_update_ram":                "Serviço %s: a memória mínima subiu de %d para %d MB.",
		"stack_update_added":              "Serviços novos: %s.",
		"stack_update_removed":            "Serviços removidos: %s.",
		"stack_update_confirm":            "Atualizar agora? Um snapshot de segurança é tirado antes.",
		"stack_update_cancelled":          "Atualização cancelada.",
		"stack_rename_args":               "Use stack rename <id> <nome> e/ou --description.",
		"stack_needs_one":                 "%s precisa de exatamente um id de stack.",
		"stack_members":                   "Membros",
		"stack_bindings":                  "Ligações",
		"stack_none":                      "nenhum",
		"block_no_new_version":            "não há versão nova",
		"block_detached":                  "a stack saiu do template",
		"block_template_removed":          "o autor removeu o template",
		"block_link_revoked":              "o link de origem foi revogado",
		"block_ram":                       "memória insuficiente",
		"block_plan":                      "o plano não permite",
		"block_image_slots":               "sem vagas de imagem",
		"block_app_building":              "um app está em build",
		"block_deploy_in_progress":        "há um deploy em andamento",
		"step_validate":                   "Validando",
		"step_snapshot":                   "Snapshot de segurança",
		"step_network":                    "Rede",
		"step_databases":                  "Bancos de dados",
		"step_blob":                       "Blob",
		"step_images":                     "Imagens",
		"step_apps":                       "Apps",
		"step_variables":                  "Variáveis",
		"step_certificates":               "Certificados",
		"step_start":                      "Iniciando",
		"step_healthcheck":                "Verificando se respondeu",
		"step_outputs":                    "Saídas",
		"step_rollback":                   "Desfazendo",
		"label_name":                      "Nome",
		"label_slug":                      "Slug",
		"label_origin":                    "Origem",
		"label_category":                  "Categoria",
		"label_version":                   "Versão",
		"label_plan":                      "Plano exigido",
		"label_ram":                       "Memória mínima",
		"label_author":                    "Autor",
		"label_services":                  "Serviços",
		"label_inputs":                    "Perguntas",
		"label_stack":                     "Cria uma stack",
		"label_fits":                      "Cabe na sua conta",
		"label_description":               "Descrição",
		"label_id":                        "ID",
		"label_status":                    "Estado",
		"label_network":                   "Rede",
		"label_template":                  "Template",
		"label_mode":                      "Modo",
		"label_uses":                      "Usos",
		"label_expires":                   "Expira em",
		"label_token":                     "Token do link",
		"label_required":                  "obrigatória",
		"label_yes":                       "sim",
		"label_no":                        "não",
		"label_update":                    "atualização disponível",
		"label_unverified":                "Não verificado pela Vertra",
		"label_never":                     "nunca",
		"label_unlimited":                 "sem limite",
		"label_reason":                    "Motivo",
		"fit_no_plan":                     "sem plano",
		"fit_ram":                         "memória insuficiente",
		"fit_web_publish":                 "sem publicação web",
		"fit_image_plan":                  "plano sem imagens",
		"fit_image_slots":                 "sem vagas de imagem",
		"fit_private_network":             "rede privada indisponível",
		"fit_blob_plan":                   "plano sem Blob",
		"fit_min_plan":                    "plano abaixo do mínimo",
		"fit_max_apps":                    "limite de apps",
		"fit_max_databases":               "limite de bancos",
		"fit_deploys_per_hour":            "limite de deploys por hora",
		"page_hint":                       "Página %d de %d (use --page %d para a próxima).",
	},
	"en": {
		"spin_pack":        "Packing the code…",
		"spin_upload":      "Uploading the template…",
		"spin_validate":    "Validating the manifest…",
		"manifest_ok":      "Manifest is valid.",
		"manifest_bad":     "Manifest is invalid: %d issue(s).",
		"yaml_syntax":      "Invalid YAML",
		"deploy_started":   "Creating %s…",
		"deploy_done":      "Done.",
		"deploy_failed":    "The deploy failed and was undone.",
		"deploy_still":     "The deploy is still running. Follow it in the dashboard; id %s.",
		"outputs":          "Outputs",
		"secret_once":      "This value is shown only now and cannot be read again. Save it.",
		"output_read":      "(already read; not shown again)",
		"created":          "Created",
		"resources":        "Resources created",
		"ask_name":         "Name of the app or stack",
		"ask_yes_no":       "y/n",
		"ask_choose":       "Choose (number or value)",
		"ask_required":     "An answer is required.",
		"ask_invalid":      "Invalid answer: %s",
		"missing_answers":  "Missing answers (use --set key=value or run in a terminal): %s",
		"unknown_inputs":   "Unknown question in --set: %s. Questions of this template: %s",
		"bad_set":          "Use --set key=value (got %q).",
		"bad_service":      "Use --service <service>.<name|ram|subdomain>=<value> (got %q).",
		"unknown_service":  "Unknown service in --service: %s. Services: %s",
		"name_required":    "Give --name.",
		"need_template":    "Give the template slug, or --share with the link.",
		"v_not_number":     "not a number",
		"v_not_bool":       "use yes/no (true/false)",
		"v_not_option":     "accepted values: %s",
		"v_min_length":     "at least %d characters",
		"v_max_length":     "at most %d characters",
		"v_min":            "minimum %v",
		"v_max":            "maximum %v",
		"v_regex":          "invalid format",
		"share_once":       "The link token is shown only now. Save it; whoever has it can use the template.",
		"share_mode_bad":   "--mode must be use or full_copy.",
		"share_revoked":    "Link revoked.",
		"pack_too_large":   "The service code is over 100 MB. Nothing was sent; use a .vertraignore to leave files out.",
		"secret_in_source": "The code sent has files that hold secrets; remove them (or add them to .vertraignore): %s",
		"changelog_hint":   "Tip: use --changelog to describe the version.",
		"created_hint":     "Use it with: vertra template deploy %s --name <name>",
		"newver_hint":      "Version %s published.",

		"TEMPLATE_NOT_FOUND":              "Template not found.",
		"TEMPLATE_VERSION_NOT_FOUND":      "Template version not found.",
		"TEMPLATE_SLUG_TAKEN":             "You already have a template with this id (slug).",
		"TEMPLATE_INPUT_INVALID":          "Some answer is invalid",
		"TEMPLATE_SOURCE_REQUIRED":        "The code of a service of type code is missing.",
		"TEMPLATE_PLAN_REQUIRED":          "Your plan cannot use this template.",
		"TEMPLATE_RAM_INSUFFICIENT":       "Not enough memory in your account for this template.",
		"TEMPLATE_IMAGE_SLOTS_EXHAUSTED":  "You have no image slots left.",
		"TEMPLATE_CLOSED":                 "This template is closed: its code is not accessible.",
		"TEMPLATE_PRIVATE_LIMIT_REACHED":  "You reached the limit of private templates.",
		"TEMPLATE_DEPLOY_IN_PROGRESS":     "A deploy of this template is already running.",
		"TEMPLATE_DEPLOY_FAILED":          "The deploy failed and was undone.",
		"TEMPLATE_DEPLOY_NOT_FOUND":       "Deploy not found.",
		"IDEMPOTENCY_KEY_CONFLICT":        "This idempotency key was already used with a different request.",
		"TEMPLATE_UPDATE_BLOCKED":         "The update cannot run now",
		"TEMPLATE_DEPRECATED":             "This template was deprecated.",
		"TEMPLATE_VERSION_NOT_SELECTABLE": "This template does not allow choosing the version.",
		"TEMPLATE_STORAGE_UNAVAILABLE":    "Template storage is unavailable. Try again shortly.",
		"SHARE_LINK_NOT_FOUND":            "Link not found.",
		"SHARE_LINK_REVOKED":              "This link was revoked.",
		"SHARE_LINK_EXPIRED":              "This link expired.",
		"SHARE_LINK_EXHAUSTED":            "This link has been used the maximum number of times.",
		"STACK_NOT_FOUND":                 "Stack not found.",
		"STACK_NAME_MISMATCH":             "The name typed is not the stack name.",
		"STACK_NETWORK_MODE_UNAVAILABLE":  "That network mode is not available yet.",
		"APP_BUILDING":                    "An app of the stack is still building. Wait for it to finish.",
		"stack_confirm_required":          "Confirmation required to delete stack %q: run again with --yes.",
		"stack_confirm_warn_all":          "Deleting stack %q also deletes its %d members (apps, databases and Blob as set by --blob).",
		"stack_confirm_warn_dissolve":     "Deleting stack %q removes only the group: its %d members stay as loose resources.",
		"stack_confirm_prompt":            "Type the stack name (%s) to confirm: ",
		"stack_blob_needs_all":            "--blob only applies with --delete-all (without it, Blob files are kept).",
		"stack_blob_bad":                  "--blob must be keep or delete.",
		"stack_update_none":               "The stack is already on the latest template version (%s).",
		"stack_update_no_template":        "This stack did not come from a template.",
		"stack_update_blocked":            "Update blocked: %s.",
		"stack_update_available":          "Update available: %s → %s.",
		"stack_update_security":           "This version fixes a security problem.",
		"stack_update_permissions":        "This version gives the template more permissions.",
		"stack_update_ram":                "Service %s: the minimum memory rose from %d to %d MB.",
		"stack_update_added":              "New services: %s.",
		"stack_update_removed":            "Removed services: %s.",
		"stack_update_confirm":            "Update now? A safety snapshot is taken first.",
		"stack_update_cancelled":          "Update cancelled.",
		"stack_rename_args":               "Use stack rename <id> <name> and/or --description.",
		"stack_needs_one":                 "%s needs exactly one stack id.",
		"stack_members":                   "Members",
		"stack_bindings":                  "Connections",
		"stack_none":                      "none",
		"block_no_new_version":            "there is no new version",
		"block_detached":                  "the stack left the template",
		"block_template_removed":          "the author removed the template",
		"block_link_revoked":              "the source link was revoked",
		"block_ram":                       "not enough memory",
		"block_plan":                      "your plan does not allow it",
		"block_image_slots":               "no image slots",
		"block_app_building":              "an app is building",
		"block_deploy_in_progress":        "a deploy is running",
		"step_validate":                   "Validating",
		"step_snapshot":                   "Safety snapshot",
		"step_network":                    "Network",
		"step_databases":                  "Databases",
		"step_blob":                       "Blob",
		"step_images":                     "Images",
		"step_apps":                       "Apps",
		"step_variables":                  "Variables",
		"step_certificates":               "Certificates",
		"step_start":                      "Starting",
		"step_healthcheck":                "Checking it answers",
		"step_outputs":                    "Outputs",
		"step_rollback":                   "Undoing",
		"label_name":                      "Name",
		"label_slug":                      "Slug",
		"label_origin":                    "Origin",
		"label_category":                  "Category",
		"label_version":                   "Version",
		"label_plan":                      "Required plan",
		"label_ram":                       "Minimum memory",
		"label_author":                    "Author",
		"label_services":                  "Services",
		"label_inputs":                    "Questions",
		"label_stack":                     "Creates a stack",
		"label_fits":                      "Fits your account",
		"label_description":               "Description",
		"label_id":                        "ID",
		"label_status":                    "Status",
		"label_network":                   "Network",
		"label_template":                  "Template",
		"label_mode":                      "Mode",
		"label_uses":                      "Uses",
		"label_expires":                   "Expires",
		"label_token":                     "Link token",
		"label_required":                  "required",
		"label_yes":                       "yes",
		"label_no":                        "no",
		"label_update":                    "update available",
		"label_unverified":                "Not verified by Vertra",
		"label_never":                     "never",
		"label_unlimited":                 "unlimited",
		"label_reason":                    "Reason",
		"fit_no_plan":                     "no plan",
		"fit_ram":                         "not enough memory",
		"fit_web_publish":                 "no web publishing",
		"fit_image_plan":                  "plan without images",
		"fit_image_slots":                 "no image slots",
		"fit_private_network":             "private network unavailable",
		"fit_blob_plan":                   "plan without Blob",
		"fit_min_plan":                    "plan below the minimum",
		"fit_max_apps":                    "apps limit",
		"fit_max_databases":               "databases limit",
		"fit_deploys_per_hour":            "deploys per hour limit",
		"page_hint":                       "Page %d of %d (use --page %d for the next one).",
	},
	"es": {
		"spin_pack":        "Empaquetando el código…",
		"spin_upload":      "Subiendo la plantilla…",
		"spin_validate":    "Validando el manifiesto…",
		"manifest_ok":      "Manifiesto válido.",
		"manifest_bad":     "Manifiesto inválido: %d problema(s).",
		"yaml_syntax":      "YAML inválido",
		"deploy_started":   "Creando %s…",
		"deploy_done":      "Completado.",
		"deploy_failed":    "El deploy falló y se deshizo.",
		"deploy_still":     "El deploy sigue en curso. Síguelo en el panel; id %s.",
		"outputs":          "Salidas",
		"secret_once":      "Este valor se muestra solo ahora y no se podrá leer de nuevo. Guárdalo.",
		"output_read":      "(ya leído; no se muestra de nuevo)",
		"created":          "Creado",
		"resources":        "Recursos creados",
		"ask_name":         "Nombre de la app o del stack",
		"ask_yes_no":       "s/n",
		"ask_choose":       "Elige (número o valor)",
		"ask_required":     "La respuesta es obligatoria.",
		"ask_invalid":      "Respuesta inválida: %s",
		"missing_answers":  "Faltan respuestas (usa --set clave=valor o ejecuta en una terminal): %s",
		"unknown_inputs":   "Pregunta desconocida en --set: %s. Preguntas de esta plantilla: %s",
		"bad_set":          "Usa --set clave=valor (recibí %q).",
		"bad_service":      "Usa --service <servicio>.<name|ram|subdomain>=<valor> (recibí %q).",
		"unknown_service":  "Servicio desconocido en --service: %s. Servicios: %s",
		"name_required":    "Indica --name.",
		"need_template":    "Indica el slug de la plantilla, o --share con el enlace.",
		"v_not_number":     "no es un número",
		"v_not_bool":       "usa sí/no (true/false)",
		"v_not_option":     "valores aceptados: %s",
		"v_min_length":     "mínimo %d caracteres",
		"v_max_length":     "máximo %d caracteres",
		"v_min":            "mínimo %v",
		"v_max":            "máximo %v",
		"v_regex":          "formato inválido",
		"share_once":       "El token del enlace se muestra solo ahora. Guárdalo; quien lo tenga puede usar la plantilla.",
		"share_mode_bad":   "--mode debe ser use o full_copy.",
		"share_revoked":    "Enlace revocado.",
		"pack_too_large":   "El código del servicio supera los 100 MB. No se envió nada; usa un .vertraignore para excluir archivos.",
		"secret_in_source": "El código enviado tiene archivos con secretos; quítalos (o ponlos en .vertraignore): %s",
		"changelog_hint":   "Consejo: usa --changelog para describir la versión.",
		"created_hint":     "Úsala con: vertra template deploy %s --name <nombre>",
		"newver_hint":      "Versión %s publicada.",

		"TEMPLATE_NOT_FOUND":              "Plantilla no encontrada.",
		"TEMPLATE_VERSION_NOT_FOUND":      "Versión de la plantilla no encontrada.",
		"TEMPLATE_SLUG_TAKEN":             "Ya tienes una plantilla con este id (slug).",
		"TEMPLATE_INPUT_INVALID":          "Alguna respuesta es inválida",
		"TEMPLATE_SOURCE_REQUIRED":        "Falta el código de un servicio de tipo code.",
		"TEMPLATE_PLAN_REQUIRED":          "Tu plan no permite usar esta plantilla.",
		"TEMPLATE_RAM_INSUFFICIENT":       "Memoria insuficiente en tu cuenta para esta plantilla.",
		"TEMPLATE_IMAGE_SLOTS_EXHAUSTED":  "Se acabaron tus cupos de imagen.",
		"TEMPLATE_CLOSED":                 "Esta plantilla es cerrada: su código no es accesible.",
		"TEMPLATE_PRIVATE_LIMIT_REACHED":  "Llegaste al límite de plantillas privadas.",
		"TEMPLATE_DEPLOY_IN_PROGRESS":     "Ya hay un deploy de esta plantilla en curso.",
		"TEMPLATE_DEPLOY_FAILED":          "El deploy falló y se deshizo.",
		"TEMPLATE_DEPLOY_NOT_FOUND":       "Deploy no encontrado.",
		"IDEMPOTENCY_KEY_CONFLICT":        "Esta clave de idempotencia ya se usó con otra solicitud.",
		"TEMPLATE_UPDATE_BLOCKED":         "La actualización no se puede hacer ahora",
		"TEMPLATE_DEPRECATED":             "Esta plantilla fue descontinuada.",
		"TEMPLATE_VERSION_NOT_SELECTABLE": "Esta plantilla no permite elegir la versión.",
		"TEMPLATE_STORAGE_UNAVAILABLE":    "El almacenamiento de plantillas no está disponible. Inténtalo en unos instantes.",
		"SHARE_LINK_NOT_FOUND":            "Enlace no encontrado.",
		"SHARE_LINK_REVOKED":              "Este enlace fue revocado.",
		"SHARE_LINK_EXPIRED":              "Este enlace expiró.",
		"SHARE_LINK_EXHAUSTED":            "Este enlace ya se usó el máximo de veces.",
		"STACK_NOT_FOUND":                 "Stack no encontrado.",
		"STACK_NAME_MISMATCH":             "El nombre escrito no es el nombre del stack.",
		"STACK_NETWORK_MODE_UNAVAILABLE":  "Ese modo de red aún no está disponible.",
		"APP_BUILDING":                    "Una app del stack aún se está construyendo. Espera a que termine.",
		"stack_confirm_required":          "Se necesita confirmación para eliminar el stack %q: ejecuta de nuevo con --yes.",
		"stack_confirm_warn_all":          "Eliminar el stack %q también elimina sus %d miembros (apps, bases de datos y Blob según --blob).",
		"stack_confirm_warn_dissolve":     "Eliminar el stack %q deshace solo el grupo: sus %d miembros siguen como recursos sueltos.",
		"stack_confirm_prompt":            "Escribe el nombre del stack (%s) para confirmar: ",
		"stack_blob_needs_all":            "--blob solo vale con --delete-all (sin él, los archivos de Blob se mantienen).",
		"stack_blob_bad":                  "--blob debe ser keep o delete.",
		"stack_update_none":               "El stack ya está en la última versión de la plantilla (%s).",
		"stack_update_no_template":        "Este stack no vino de una plantilla.",
		"stack_update_blocked":            "Actualización bloqueada: %s.",
		"stack_update_available":          "Actualización disponible: %s → %s.",
		"stack_update_security":           "Esta versión corrige un problema de seguridad.",
		"stack_update_permissions":        "Esta versión concede más permisos a la plantilla.",
		"stack_update_ram":                "Servicio %s: la memoria mínima subió de %d a %d MB.",
		"stack_update_added":              "Servicios nuevos: %s.",
		"stack_update_removed":            "Servicios eliminados: %s.",
		"stack_update_confirm":            "¿Actualizar ahora? Antes se toma un snapshot de seguridad.",
		"stack_update_cancelled":          "Actualización cancelada.",
		"stack_rename_args":               "Usa stack rename <id> <nombre> y/o --description.",
		"stack_needs_one":                 "%s necesita exactamente un id de stack.",
		"stack_members":                   "Miembros",
		"stack_bindings":                  "Conexiones",
		"stack_none":                      "ninguno",
		"block_no_new_version":            "no hay versión nueva",
		"block_detached":                  "el stack salió de la plantilla",
		"block_template_removed":          "el autor eliminó la plantilla",
		"block_link_revoked":              "el enlace de origen fue revocado",
		"block_ram":                       "memoria insuficiente",
		"block_plan":                      "tu plan no lo permite",
		"block_image_slots":               "sin cupos de imagen",
		"block_app_building":              "una app se está construyendo",
		"block_deploy_in_progress":        "hay un deploy en curso",
		"step_validate":                   "Validando",
		"step_snapshot":                   "Snapshot de seguridad",
		"step_network":                    "Red",
		"step_databases":                  "Bases de datos",
		"step_blob":                       "Blob",
		"step_images":                     "Imágenes",
		"step_apps":                       "Apps",
		"step_variables":                  "Variables",
		"step_certificates":               "Certificados",
		"step_start":                      "Iniciando",
		"step_healthcheck":                "Comprobando que responde",
		"step_outputs":                    "Salidas",
		"step_rollback":                   "Deshaciendo",
		"label_name":                      "Nombre",
		"label_slug":                      "Slug",
		"label_origin":                    "Origen",
		"label_category":                  "Categoría",
		"label_version":                   "Versión",
		"label_plan":                      "Plan requerido",
		"label_ram":                       "Memoria mínima",
		"label_author":                    "Autor",
		"label_services":                  "Servicios",
		"label_inputs":                    "Preguntas",
		"label_stack":                     "Crea un stack",
		"label_fits":                      "Cabe en tu cuenta",
		"label_description":               "Descripción",
		"label_id":                        "ID",
		"label_status":                    "Estado",
		"label_network":                   "Red",
		"label_template":                  "Plantilla",
		"label_mode":                      "Modo",
		"label_uses":                      "Usos",
		"label_expires":                   "Expira",
		"label_token":                     "Token del enlace",
		"label_required":                  "obligatoria",
		"label_yes":                       "sí",
		"label_no":                        "no",
		"label_update":                    "actualización disponible",
		"label_unverified":                "No verificado por Vertra",
		"label_never":                     "nunca",
		"label_unlimited":                 "sin límite",
		"label_reason":                    "Motivo",
		"fit_no_plan":                     "sin plan",
		"fit_ram":                         "memoria insuficiente",
		"fit_web_publish":                 "sin publicación web",
		"fit_image_plan":                  "plan sin imágenes",
		"fit_image_slots":                 "sin cupos de imagen",
		"fit_private_network":             "red privada no disponible",
		"fit_blob_plan":                   "plan sin Blob",
		"fit_min_plan":                    "plan por debajo del mínimo",
		"fit_max_apps":                    "límite de apps",
		"fit_max_databases":               "límite de bases de datos",
		"fit_deploys_per_hour":            "límite de deploys por hora",
		"page_hint":                       "Página %d de %d (usa --page %d para la siguiente).",
	},
}

// Text returns the message for key in the current language, falling back to English.
func Text(key string) string {
	if m := messages[ui.Locale]; m != nil && m[key] != "" {
		return m[key]
	}
	return messages["en"][key]
}

func text(key string) string { return Text(key) }

// I18n picks the text of the current language, falling back to pt-br (the one that is always there).
func I18n(t vertracloud.TemplateI18nText) string {
	switch ui.Locale {
	case "en":
		if t.EN != "" {
			return t.EN
		}
	case "es":
		if t.ES != "" {
			return t.ES
		}
	}
	return t.PtBR
}

// Friendly swaps the API message of the known template, share link and stack
// errors for one in the current language, keeping status, code and details.
// The findings of TEMPLATE_INPUT_INVALID and TEMPLATE_UPDATE_BLOCKED are
// appended. Anything else is returned as it came.
func Friendly(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, ErrSourceTooLarge):
		return errors.New(text("pack_too_large"))
	}
	apiErr, ok := rest.AsAPIError(err)
	if !ok {
		return err
	}
	msg := text(apiErr.Code)
	if msg == "" {
		return err
	}
	switch apiErr.Code {
	case vertracloud.ErrCodeTemplateInputInvalid:
		if issues := detailIssues(apiErr.Details); len(issues) > 0 {
			msg += ": " + strings.Join(issues, "; ")
		}
	case vertracloud.ErrCodeTemplateUpdateBlocked:
		if b, ok := vertracloud.TemplateUpdateBlockedDetails(err); ok && b.Reason != "" {
			msg += ": " + BlockReason(b.Reason)
		}
	}
	c := *apiErr
	c.Message = msg
	return &c
}

// BlockReason is the text of why an update is blocked.
func BlockReason(r vertracloud.TemplateUpdateBlockReason) string {
	if t := text("block_" + string(r)); t != "" {
		return t
	}
	return string(r)
}

// detailIssues reads details.issues of TEMPLATE_INPUT_INVALID without
// assuming more than a list of objects with an id/input/path and a message/code.
func detailIssues(details any) []string {
	m, _ := details.(map[string]any)
	list, _ := m["issues"].([]any)
	var out []string
	for _, item := range list {
		issue, _ := item.(map[string]any)
		if issue == nil {
			continue
		}
		where := firstString(issue, "input", "input_id", "id", "path")
		what := firstString(issue, "message", "code")
		switch {
		case where != "" && what != "":
			out = append(out, fmt.Sprintf("%s (%s)", where, what))
		case where != "":
			out = append(out, where)
		case what != "":
			out = append(out, what)
		}
	}
	return out
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}
