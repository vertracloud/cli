package main

import (
	"fmt"
	"time"

	"github.com/vertracloud/cli/internal/ui"
	"github.com/vertracloud/sdk-api-go/rest"
	"github.com/vertracloud/sdk-api-go/vertracloud"
)

// snapshotFailure gives the snapshot, restore and download errors a clear
// message and a hint in the CLI language. ok is false for any other error.
func snapshotFailure(err error, apiErr *rest.APIError) (message, hint string, ok bool) {
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
	code := " " + ui.Dim("("+ui.Plain(apiErr.Code)+")")
	switch apiErr.Code {
	case vertracloud.ErrCodeRestoreInProgress:
		return pick("Este recurso está sendo restaurado agora.", "This resource is being restored right now.", "Este recurso se está restaurando ahora.") + code,
			pick("Espere a restauração terminar e tente de novo. Veja as restaurações em andamento com: vertra snapshot jobs <id> --kind restore", "Wait for the restore to finish, then try again. List the restore jobs with: vertra snapshot jobs <id> --kind restore", "Espera a que la restauración termine y vuelve a intentar. Mira los jobs de restauración con: vertra snapshot jobs <id> --kind restore"), true
	case vertracloud.ErrCodeSnapshotExceedsStorage:
		msg := pick("O snapshot não cabe no destino.", "The snapshot does not fit in the target.", "El snapshot no cabe en el destino.") + code
		details, found := vertracloud.SnapshotExceedsStorageDetails(err)
		if !found || details.RequiredBytes <= 0 {
			return msg, pick("Restaure em um destino com mais espaço.", "Restore into a target with more space.", "Restaura en un destino con más espacio."), true
		}
		hint = fmt.Sprintf(pick("O snapshot precisa de %s e o destino tem %s.", "The snapshot needs %s and the target has %s.", "El snapshot necesita %s y el destino tiene %s."), formatBytes(details.RequiredBytes), formatBytes(details.AvailableBytes))
		if details.RequiredRAMMB > 0 {
			hint += " " + fmt.Sprintf(pick("Use pelo menos %d MB de RAM.", "Use at least %d MB of RAM.", "Usa al menos %d MB de RAM."), details.RequiredRAMMB)
		}
		return msg, hint, true
	case vertracloud.ErrCodeSnapshotRateLimited:
		details, found := vertracloud.SnapshotRateLimitDetails(err)
		hint = pick("Tente de novo em instantes.", "Try again in a moment.", "Inténtalo de nuevo en un momento.")
		if found && details.Scope == vertracloud.SnapshotRateLimitScopeAccount && details.ResetsAt != nil {
			hint = fmt.Sprintf(pick("A cota de snapshots da conta libera às %s.", "The account's snapshot quota frees up at %s.", "La cuota de snapshots de la cuenta se libera a las %s."), details.ResetsAt.Local().Format("2006-01-02 15:04 MST"))
		} else if found && details.RetryAfter > 0 {
			hint = fmt.Sprintf(pick("Tente de novo em %d s.", "Try again in %d s.", "Inténtalo de nuevo en %d s."), seconds(time.Duration(details.RetryAfter*float64(time.Second))))
		}
		return pick("Limite de snapshots atingido.", "Snapshot limit reached.", "Límite de snapshots alcanzado.") + code, hint, true
	case vertracloud.ErrCodeRestoreCooldown:
		hint = pick("Espere um pouco antes de restaurar de novo.", "Wait a little before restoring again.", "Espera un poco antes de restaurar de nuevo.")
		if wait, found := vertracloud.RestoreCooldownRetryAfter(err); found && wait > 0 {
			hint = fmt.Sprintf(pick("Tente restaurar de novo em %d s.", "Try restoring again in %d s.", "Intenta restaurar de nuevo en %d s."), seconds(wait))
		}
		return pick("Há uma espera entre restaurações.", "There is a wait between restores.", "Hay una espera entre restauraciones.") + code, hint, true
	case vertracloud.ErrCodeSnapshotTooLarge:
		msg := pick("O conteúdo é grande demais para um snapshot.", "The content is too large for a snapshot.", "El contenido es demasiado grande para un snapshot.") + code
		hint = pick("Remova arquivos que não precisa guardar e tente de novo.", "Remove files you do not need to keep and try again.", "Elimina archivos que no necesites guardar e inténtalo de nuevo.")
		if limit, found := vertracloud.SnapshotTooLargeMaxBytes(err); found && limit > 0 {
			hint = fmt.Sprintf(pick("O limite é %s. Remova arquivos que não precisa guardar e tente de novo.", "The limit is %s. Remove files you do not need to keep and try again.", "El límite es %s. Elimina archivos que no necesites guardar e inténtalo de nuevo."), formatBytes(limit))
		}
		return msg, hint, true
	case vertracloud.ErrCodeAppBuilding:
		return pick("A aplicação está em deploy agora.", "The application is being deployed right now.", "La aplicación se está desplegando ahora.") + code,
			pick("Espere o build terminar e tente de novo.", "Wait for the build to finish, then try again.", "Espera a que termine el build y vuelve a intentar."), true
	case vertracloud.ErrCodeOperationInProgress:
		return pick("Snapshot em andamento neste recurso.", "A snapshot is in progress on this resource.", "Hay un snapshot en curso en este recurso.") + code,
			pick("Tente de novo em instantes.", "Try again in a moment.", "Inténtalo de nuevo en un momento."), true
	case vertracloud.ErrCodeDatabaseNotReady:
		return pick("O banco de dados ainda não está pronto.", "The database is not ready yet.", "La base de datos aún no está lista.") + code,
			pick("Espere o banco ficar online e tente de novo.", "Wait for the database to come online, then try again.", "Espera a que la base esté en línea y vuelve a intentar."), true
	case vertracloud.ErrCodeSnapshotCorrupted:
		return pick("O snapshot está corrompido e não pôde ser restaurado.", "The snapshot is corrupted and could not be restored.", "El snapshot está corrupto y no se pudo restaurar.") + code,
			pick("Tente outro snapshot.", "Try another snapshot.", "Prueba con otro snapshot."), true
	case vertracloud.ErrCodeSnapshotTimeout:
		return pick("A operação do snapshot demorou demais.", "The snapshot operation took too long.", "La operación del snapshot tardó demasiado.") + code,
			pick("Tente de novo; se repetir, fale com o suporte.", "Try again; if it repeats, contact support.", "Inténtalo de nuevo; si se repite, contacta con soporte."), true
	case vertracloud.ErrCodeSnapshotImportFailed:
		return pick("Os dados do snapshot não puderam ser importados no banco.", "The snapshot data could not be imported into the database.", "Los datos del snapshot no se pudieron importar en la base.") + code,
			pick("O banco continua como estava. Confira se o snapshot é deste tipo de banco.", "The database is unchanged. Check that the snapshot is from this kind of database.", "La base queda como estaba. Comprueba que el snapshot sea de este tipo de base."), true
	case vertracloud.ErrCodeSnapshotEngineMismatch:
		return pick("O snapshot é de outro tipo de banco.", "The snapshot is from a different database engine.", "El snapshot es de otro motor de base de datos.") + code,
			pick("Restaure em um banco do mesmo tipo.", "Restore into a database of the same type.", "Restaura en una base del mismo tipo."), true
	case vertracloud.ErrCodeSnapshotFailed:
		return pick("O job do snapshot falhou.", "The snapshot job failed.", "El job del snapshot falló.") + code,
			pick("Veja o job com: vertra snapshot job <id> <job>", "See the job with: vertra snapshot job <id> <job>", "Mira el job con: vertra snapshot job <id> <job>"), true
	}
	return "", "", false
}

func seconds(d time.Duration) int { return int((d + time.Second - 1) / time.Second) }

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value, suffix := float64(n), "B"
	for _, s := range []string{"KiB", "MiB", "GiB", "TiB"} {
		value /= unit
		suffix = s
		if value < unit {
			break
		}
	}
	return fmt.Sprintf("%.1f %s", value, suffix)
}
