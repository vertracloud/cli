package blob

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/vertracloud/cli/internal/ui"
	"github.com/vertracloud/sdk-api-go/vertracloud"
)

// Human-readable output. The JSON form is the raw SDK value; these strings
// are only for the terminal, in the CLI's three languages.

var messages = map[string][3]string{
	"empty":      {"Nenhum arquivo.", "No files.", "Ningún archivo."},
	"col_file":   {"ARQUIVO", "FILE", "ARCHIVO"},
	"col_size":   {"TAMANHO", "SIZE", "TAMAÑO"},
	"col_vis":    {"VISIBILIDADE", "VISIBILITY", "VISIBILIDAD"},
	"col_id":     {"ID", "ID", "ID"},
	"uploaded":   {"Enviado: %s (%s, %s)", "Uploaded: %s (%s, %s)", "Subido: %s (%s, %s)"},
	"downloaded": {"Baixado em %s (%s)", "Downloaded to %s (%s)", "Descargado en %s (%s)"},
	"deleted":    {"Apagados: %d · não encontrados: %d", "Deleted: %d · not found: %d", "Borrados: %d · no encontrados: %d"},
	"link":       {"Link (válido até %s):", "Link (valid until %s):", "Enlace (válido hasta %s):"},
	"files":      {"Arquivos", "Files", "Archivos"},
	"used":       {"Usado", "Used", "Usado"},
	"quota":      {"Cota", "Quota", "Cuota"},
	"over":       {"Excedente", "Over quota", "Excedente"},
	"simple_max": {"Envio simples até", "Simple upload up to", "Envío simple hasta"},
	"public":     {"público", "public", "público"},
	"private":    {"privado", "private", "privado"},
}

func t(key string) string {
	i := map[string]int{"pt": 0, "en": 1, "es": 2}[ui.Locale]
	return messages[key][i]
}

func visibilityLabel(v vertracloud.BlobVisibility) string {
	if v == vertracloud.BlobVisibilityPublic {
		return t("public")
	}
	return t("private")
}

// humanSize uses binary units with one decimal, for example "1.5 MB".
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func listText(objects []vertracloud.BlobObject, folders []string) string {
	var b strings.Builder
	for _, folder := range folders {
		b.WriteString(ui.Cyan(ui.Plain(folder)) + "\n")
	}
	if len(objects) == 0 {
		if len(folders) == 0 {
			b.WriteString(ui.Dim(t("empty")) + "\n")
		}
		return b.String()
	}
	table := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintf(table, "%s\t%s\t%s\t%s\n", t("col_file"), t("col_size"), t("col_vis"), t("col_id"))
	for _, obj := range objects {
		fmt.Fprintf(table, "%s\t%s\t%s\t%s\n", ui.Plain(obj.Path), humanSize(obj.Size), visibilityLabel(obj.Visibility), obj.ID)
	}
	_ = table.Flush()
	return b.String()
}

func uploadedText(obj vertracloud.BlobObject) string {
	text := fmt.Sprintf(t("uploaded"), ui.Plain(obj.Path), humanSize(obj.Size), visibilityLabel(obj.Visibility))
	if obj.URL != nil {
		text += "\n" + ui.Plain(*obj.URL)
	}
	return text
}

func linkText(link vertracloud.BlobDownload) string {
	if link.ExpiresAt.IsZero() {
		return ui.Plain(link.URL)
	}
	return fmt.Sprintf(t("link"), link.ExpiresAt.Local().Format("2006-01-02 15:04")) + "\n" + ui.Plain(link.URL)
}

func statsText(s vertracloud.BlobStats) string {
	lines := []string{
		fmt.Sprintf("%s: %d", t("files"), s.Objects),
		fmt.Sprintf("%s: %s / %s", t("used"), humanSize(s.UsedBytes), humanSize(s.QuotaBytes)),
	}
	if s.OverQuotaBytes > 0 {
		lines = append(lines, fmt.Sprintf("%s: %s", t("over"), humanSize(s.OverQuotaBytes)))
	}
	lines = append(lines, fmt.Sprintf("%s: %s", t("simple_max"), humanSize(s.Limits.MaxSimpleUploadBytes)))
	return strings.Join(lines, "\n")
}
