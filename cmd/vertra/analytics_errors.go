package main

import (
	"fmt"
	"time"

	"github.com/vertracloud/cli/internal/ui"
	"github.com/vertracloud/sdk-api-go/rest"
	"github.com/vertracloud/sdk-api-go/vertracloud"
)

// analyticsFailure turns the Web Analytics errors (plan, window, busy) into a
// clear message and a hint. ok is false for any other error.
func analyticsFailure(err error, apiErr *rest.APIError) (message, hint string, ok bool) {
	if apiErr == nil {
		return "", "", false
	}
	pick := func(pt, en, es string) string {
		switch ui.Locale {
		case "pt":
			return pt
		case "es":
			return es
		}
		return en
	}
	switch apiErr.Code {
	case vertracloud.ErrCodePlanDoesNotSupportWebAnalyticsPerformance:
		return pick("O plano deste app não inclui o desempenho (latência na borda e no container).", "This app's plan does not include performance (edge and container latency).", "El plan de esta app no incluye el rendimiento (latencia en el borde y en el contenedor).") + " " + ui.Dim("("+apiErr.Code+")"),
			pick("Recurso de planos maiores. Veja os planos no painel.", "A feature of larger plans. See the plans in the dashboard.", "Recurso de planes mayores. Mira los planes en el panel."), true
	case vertracloud.ErrCodePlanDoesNotSupportWebAnalyticsRequests:
		return pick("O plano deste app não inclui o log de requisições.", "This app's plan does not include the request log.", "El plan de esta app no incluye el registro de solicitudes.") + " " + ui.Dim("("+apiErr.Code+")"),
			pick("Recurso de planos maiores. Veja os planos no painel.", "A feature of larger plans. See the plans in the dashboard.", "Recurso de planes mayores. Mira los planes en el panel."), true
	case vertracloud.ErrCodeAnalyticsRangeExceedsPlan:
		details, found := vertracloud.AnalyticsRangeExceedsPlan(err)
		msg := pick("A janela começa antes do histórico que o plano permite.", "The window starts before the history your plan allows.", "La ventana empieza antes del historial que permite el plan.") + " " + ui.Dim("("+apiErr.Code+")")
		if !found {
			return msg, pick("Use um --start mais recente (e no máximo 30 dias de janela).", "Use a more recent --start (and a window of at most 30 days).", "Usa un --start más reciente (y una ventana de máximo 30 días)."), true
		}
		earliest := details.Earliest.UTC().Format(time.RFC3339)
		return msg, fmt.Sprintf(pick("O plano lê %d dias para trás: use --start %s ou depois (janela de no máximo 30 dias).", "Your plan reads %d days back: use --start %s or later (window of at most 30 days).", "El plan lee %d días atrás: usa --start %s o después (ventana de máximo 30 días)."), details.HistoryDays, earliest), true
	case "INVALID_RANGE":
		return pick("Janela inválida.", "Invalid window.", "Ventana inválida.") + " " + ui.Dim("("+apiErr.Code+")"),
			pick("Use --start antes de --end, com no máximo 30 dias de janela.", "Use --start before --end, with a window of at most 30 days.", "Usa --start antes de --end, con una ventana de máximo 30 días."), true
	case vertracloud.ErrCodeAnalyticsBusy:
		hint := pick("Tente de novo em instantes.", "Try again in a moment.", "Inténtalo de nuevo en un momento.")
		if wait, found := vertracloud.AnalyticsBusyRetryAfter(err); found && wait > 0 {
			secs := int((wait + time.Second - 1) / time.Second)
			hint = fmt.Sprintf(pick("Tente de novo em %d s.", "Try again in %d s.", "Inténtalo de nuevo en %d s."), secs)
		}
		return pick("As métricas estão ocupadas agora.", "Analytics is busy right now.", "Las métricas están ocupadas ahora.") + " " + ui.Dim("("+apiErr.Code+")"), hint, true
	}
	return "", "", false
}
