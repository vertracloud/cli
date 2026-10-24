package images

import (
	"errors"
	"fmt"
	"strings"

	"github.com/vertracloud/cli/internal/ui"
	"github.com/vertracloud/sdk-api-go/rest"
	"github.com/vertracloud/sdk-api-go/vertracloud"
)

var messages = map[string]map[string]string{
	"pt": {
		"spin_pack":                 "Empacotando o contexto…",
		"spin_upload":               "Enviando o contexto…",
		"follow_hint":               "Build iniciado. Acompanhe com: vertra image logs %s --follow",
		"confirm_required":          "Confirmação necessária para excluir a imagem %q: rode de novo com --yes.",
		"confirm_warn":              "Excluir a imagem %q também exclui os %d apps criados a partir dela, as versões e o contexto.",
		"confirm_prompt":            "Digite o nome da imagem (%s) para confirmar: ",
		"IMAGE_NOT_FOUND":           "Imagem não encontrada.",
		"IMAGE_NOT_READY":           "A imagem ainda não tem uma versão pronta. Espere o build terminar (vertra image logs <id> --follow).",
		"IMAGE_BUILD_IN_PROGRESS":   "Já há um build desta imagem em andamento. Espere terminar e tente de novo.",
		"IMAGE_NAME_MISMATCH":       "O nome digitado não é o nome da imagem.",
		"IMAGE_CONTEXT_TOO_LARGE":   "O contexto de build é grande demais (limite: 100 MB enviados, 512 MB descompactados e 50.000 arquivos). Use um .dockerignore.",
		"IMAGE_TOO_LARGE":           "A imagem construída passa de 10 GB descompactada.",
		"IMAGE_BUILDER_UNAVAILABLE": "O construtor de imagens está indisponível agora. Tente de novo em instantes.",
		"IMAGE_VERSION_NOT_FOUND":   "Versão da imagem não encontrada, ou o log dela não foi guardado.",
		"IMAGE_LABEL_REFUSED":       "A imagem tem labels que a plataforma não aceita.",
		"IMAGE_VOLUME_REFUSED":      "A imagem declara VOLUME em /app ou abaixo (ou com caminho relativo), que a plataforma não aceita.",
		"IMAGE_APP_PORT_REQUIRED":   "Informe a porta que a imagem escuta (--port), porque o app não é publicado na web.",
		"DOCKERFILE_NOT_FOUND":      "Não há um arquivo chamado exatamente Dockerfile na raiz do contexto.",
		"DOCKERFILE_REFUSED":        "O Dockerfile tem instruções que a plataforma recusa",
		"local_too_large":           "O contexto passa de 100 MB. Nada foi enviado; use um .dockerignore para tirar arquivos.",
		"local_no_dockerfile":       "Não há um arquivo chamado exatamente Dockerfile na raiz do contexto. Nada foi enviado.",
		"line":                      "linha",
		"missing_dockerfile":        "sem Dockerfile na raiz",
		"syntax_frontend":           "# syntax= aponta para um frontend não oficial",
		"run_security_insecure":     "RUN --security=insecure não é permitido",
		"run_network_host":          "RUN --network=host não é permitido",
		"mount_ssh":                 "--mount=type=ssh não é permitido",
		"mount_secret":              "--mount=type=secret não é permitido",
		"workdir_app":               "WORKDIR em /app ou abaixo: o /app é o disco persistente e esconde o que a imagem puser ali",
		"env_file_in_context":       "arquivos .env no contexto não vão para a imagem; as chaves viram variáveis do app",
		"copy_env_file":             "COPY ou ADD de um .env: o arquivo não está no contexto, o build falha nesta linha",
		"volume_outside_app":        "VOLUME fora do /app vira pasta temporária de 64 MB, apagada a cada reinício",
		"volume_refused":            "VOLUME no /app ou abaixo, ou com caminho relativo, não é permitido",
	},
	"en": {
		"spin_pack":                 "Packing the context…",
		"spin_upload":               "Uploading the context…",
		"follow_hint":               "Build started. Follow it with: vertra image logs %s --follow",
		"confirm_required":          "Confirmation required to delete image %q: run again with --yes.",
		"confirm_warn":              "Deleting image %q also deletes the %d apps created from it, its versions and its context.",
		"confirm_prompt":            "Type the image name (%s) to confirm: ",
		"IMAGE_NOT_FOUND":           "Image not found.",
		"IMAGE_NOT_READY":           "The image has no ready version yet. Wait for the build to finish (vertra image logs <id> --follow).",
		"IMAGE_BUILD_IN_PROGRESS":   "A build of this image is already running. Wait for it to finish and try again.",
		"IMAGE_NAME_MISMATCH":       "The name typed is not the image name.",
		"IMAGE_CONTEXT_TOO_LARGE":   "The build context is too large (limits: 100 MB uploaded, 512 MB uncompressed and 50,000 files). Use a .dockerignore.",
		"IMAGE_TOO_LARGE":           "The built image is larger than 10 GB uncompressed.",
		"IMAGE_BUILDER_UNAVAILABLE": "The image builder is unavailable right now. Try again shortly.",
		"IMAGE_VERSION_NOT_FOUND":   "Image version not found, or its log was not stored.",
		"IMAGE_LABEL_REFUSED":       "The image has labels the platform does not accept.",
		"IMAGE_VOLUME_REFUSED":      "The image declares a VOLUME at /app or below (or with a relative path), which the platform does not accept.",
		"IMAGE_APP_PORT_REQUIRED":   "Give the port the image listens on (--port), because the app is not published on the web.",
		"DOCKERFILE_NOT_FOUND":      "There is no file named exactly Dockerfile at the root of the context.",
		"DOCKERFILE_REFUSED":        "The Dockerfile has instructions the platform refuses",
		"local_too_large":           "The context is over 100 MB. Nothing was sent; use a .dockerignore to leave files out.",
		"local_no_dockerfile":       "There is no file named exactly Dockerfile at the root of the context. Nothing was sent.",
		"line":                      "line",
		"missing_dockerfile":        "no Dockerfile at the root",
		"syntax_frontend":           "# syntax= points to an unofficial frontend",
		"run_security_insecure":     "RUN --security=insecure is not allowed",
		"run_network_host":          "RUN --network=host is not allowed",
		"mount_ssh":                 "--mount=type=ssh is not allowed",
		"mount_secret":              "--mount=type=secret is not allowed",
		"workdir_app":               "WORKDIR at /app or below: /app is the persistent disk and hides what the image puts there",
		"env_file_in_context":       ".env files in the context do not go into the image; their keys become app variables",
		"copy_env_file":             "COPY or ADD of a .env file: the file is not in the context, so the build fails on this line",
		"volume_outside_app":        "VOLUME outside /app becomes a temporary 64 MB folder, erased on every restart",
		"volume_refused":            "VOLUME at /app or below, or with a relative path, is not allowed",
	},
	"es": {
		"spin_pack":                 "Empaquetando el contexto…",
		"spin_upload":               "Subiendo el contexto…",
		"follow_hint":               "Build iniciado. Síguelo con: vertra image logs %s --follow",
		"confirm_required":          "Se necesita confirmación para eliminar la imagen %q: ejecuta de nuevo con --yes.",
		"confirm_warn":              "Eliminar la imagen %q también elimina las %d apps creadas desde ella, sus versiones y su contexto.",
		"confirm_prompt":            "Escribe el nombre de la imagen (%s) para confirmar: ",
		"IMAGE_NOT_FOUND":           "Imagen no encontrada.",
		"IMAGE_NOT_READY":           "La imagen aún no tiene una versión lista. Espera a que termine el build (vertra image logs <id> --follow).",
		"IMAGE_BUILD_IN_PROGRESS":   "Ya hay un build de esta imagen en curso. Espera a que termine e inténtalo de nuevo.",
		"IMAGE_NAME_MISMATCH":       "El nombre escrito no es el nombre de la imagen.",
		"IMAGE_CONTEXT_TOO_LARGE":   "El contexto de build es demasiado grande (límites: 100 MB subidos, 512 MB descomprimidos y 50.000 archivos). Usa un .dockerignore.",
		"IMAGE_TOO_LARGE":           "La imagen construida supera los 10 GB descomprimida.",
		"IMAGE_BUILDER_UNAVAILABLE": "El constructor de imágenes no está disponible ahora. Inténtalo en unos instantes.",
		"IMAGE_VERSION_NOT_FOUND":   "Versión de la imagen no encontrada, o su log no se guardó.",
		"IMAGE_LABEL_REFUSED":       "La imagen tiene labels que la plataforma no acepta.",
		"IMAGE_VOLUME_REFUSED":      "La imagen declara un VOLUME en /app o debajo (o con ruta relativa), que la plataforma no acepta.",
		"IMAGE_APP_PORT_REQUIRED":   "Indica el puerto que escucha la imagen (--port), porque la app no se publica en la web.",
		"DOCKERFILE_NOT_FOUND":      "No hay un archivo llamado exactamente Dockerfile en la raíz del contexto.",
		"DOCKERFILE_REFUSED":        "El Dockerfile tiene instrucciones que la plataforma rechaza",
		"local_too_large":           "El contexto supera los 100 MB. No se envió nada; usa un .dockerignore para excluir archivos.",
		"local_no_dockerfile":       "No hay un archivo llamado exactamente Dockerfile en la raíz del contexto. No se envió nada.",
		"line":                      "línea",
		"missing_dockerfile":        "sin Dockerfile en la raíz",
		"syntax_frontend":           "# syntax= apunta a un frontend no oficial",
		"run_security_insecure":     "RUN --security=insecure no está permitido",
		"run_network_host":          "RUN --network=host no está permitido",
		"mount_ssh":                 "--mount=type=ssh no está permitido",
		"mount_secret":              "--mount=type=secret no está permitido",
		"workdir_app":               "WORKDIR en /app o debajo: /app es el disco persistente y oculta lo que la imagen ponga ahí",
		"env_file_in_context":       "los archivos .env del contexto no van a la imagen; sus claves pasan a ser variables de la app",
		"copy_env_file":             "COPY o ADD de un .env: el archivo no está en el contexto, el build falla en esta línea",
		"volume_outside_app":        "VOLUME fuera de /app se convierte en una carpeta temporal de 64 MB, borrada en cada reinicio",
		"volume_refused":            "VOLUME en /app o debajo, o con ruta relativa, no está permitido",
	},
}

func text(key string) string {
	if m := messages[ui.Locale]; m != nil && m[key] != "" {
		return m[key]
	}
	return messages["en"][key]
}

// friendly swaps the API message of known image errors for a localized one,
// keeping status, code and details, and turns the local context errors into
// readable ones. Anything else is returned as it came.
func friendly(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, ErrContextTooLarge):
		return errors.New(text("local_too_large"))
	case errors.Is(err, ErrNoDockerfile):
		return errors.New(text("local_no_dockerfile"))
	}
	var apiErr *rest.APIError
	if !errors.As(err, &apiErr) || (!strings.HasPrefix(apiErr.Code, "IMAGE_") && !strings.HasPrefix(apiErr.Code, "DOCKERFILE_")) {
		return err
	}
	msg := text(apiErr.Code)
	if msg == "" {
		return err
	}
	if apiErr.Code == "DOCKERFILE_REFUSED" {
		if issues := refusalIssues(err); len(issues) > 0 {
			msg += ": " + strings.Join(issues, "; ")
		}
	}
	c := *apiErr
	c.Message = msg
	return &c
}

// refusalIssues lists the findings of a DOCKERFILE_REFUSED error, with the line when there is one.
func refusalIssues(err error) []string {
	found, _ := vertracloud.DockerfileRefusedIssues(err)
	var out []string
	for _, issue := range found {
		label := text(string(issue.Code))
		if label == "" {
			label = string(issue.Code)
		}
		if issue.Line != nil {
			label = fmt.Sprintf("%s %d: %s", text("line"), *issue.Line, label)
		}
		out = append(out, label)
	}
	return out
}
