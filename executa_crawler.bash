#!/usr/bin/env bash
#
# /home/galego/gal_puppeteer/executa_crawler.bash
# Lanzamento do crawler Puppeteer
# Versión 0.1.0
#

set -euo pipefail

PATH="/usr/local/bin:/usr/bin:/bin:${PATH}"

home_crawler="/home/galego/RVGD-puppeteer"
rexistro="${home_crawler}/logs/monitor.log"
script_crawler="${home_crawler}/src/crawler.js"
config_ficheiro="${home_crawler}/config.json"

echo "Iniciando o crawler Puppeteer con rexistros en ${rexistro}"

mkdir -p "$(dirname "${rexistro}")"

now_epoch="$(date +%s)"
timestamp="$(date -d "@${now_epoch}" '+%Y-%m-%d %H:%M:%S')"
# Atraso aleatorio: máximo 5 h 55 min (21300 s)
atraso_max_segundos=$((5 * 60 * 60 + 55 * 60))
atraso_segundos=$((((RANDOM << 15) | RANDOM) % (atraso_max_segundos + 1)))
atraso_minutos=$((atraso_segundos / 60))
atraso_formatado="$(printf '%02d:%02d:%02d' $((atraso_segundos / 3600)) $(((atraso_segundos % 3600) / 60)) $((atraso_segundos % 60)))"
start_epoch=$((now_epoch + atraso_segundos))
start_timestamp="$(date -d "@${start_epoch}" '+%Y-%m-%d %H:%M:%S')"

{
  echo ""
  echo "[$timestamp] ========================================"
  echo "[$timestamp] Atraso ata a execución: ${atraso_formatado} (execución prevista en ${start_timestamp})"
} >> "${rexistro}"

if (( atraso_segundos > 0 )); then
  echo "Inicio previsto en ${start_timestamp} (en ${atraso_formatado})."
  sleep "${atraso_segundos}"
fi

(
  cd "${home_crawler}" || exit 1

  node_bin="$(command -v node || true)"
  if [[ -z "${node_bin}" ]]; then
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] ERROR: Node.js non se atopou no PATH." >> "${rexistro}"
    exit 127
  fi

  if [[ ! -f "${script_crawler}" ]]; then
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] ERROR: Non se atopou ${script_crawler}." >> "${rexistro}"
    exit 2
  fi

  export PUPPETEER_EXECUTABLE_PATH="${PUPPETEER_EXECUTABLE_PATH:-/usr/bin/chromium}"

  headless_raw="${PUPPETEER_HEADLESS:-}"
  headless_orixe="contorno"
  if [[ -z "${headless_raw}" && -f "${config_ficheiro}" ]]; then
    headless_raw="$("${node_bin}" -e "const fs=require('fs');let valor='';try{const cfg=JSON.parse(fs.readFileSync(process.argv[1],'utf8'));const hb=cfg?.browser?.headless;if(typeof hb==='string'){valor=hb;}else if(typeof hb==='boolean'){valor=hb?'true':'false';}else if(hb!==undefined&&hb!==null){valor=String(hb);}}catch(_){}process.stdout.write(valor);" "${config_ficheiro}")"
    headless_orixe="config"
  fi

  headless_raw="${headless_raw//[$'\r\n']}"
  headless_lower="$(printf '%s' "${headless_raw}" | tr '[:upper:]' '[:lower:]')"
  headless_mode="old"

  notes=()

  case "${headless_lower}" in
    ""|"1"|"true"|"yes"|"old"|"legacy")
      headless_mode="old"
      ;;
    "new"|"chrome")
      headless_mode="new"
      ;;
    "false"|"0"|"no"|"shell")
      notes+=("valor headless \"${headless_raw:-<baleiro>}\" non é compatible, aplicando \"old\"")
      headless_mode="old"
      ;;
    *)
      if [[ -n "${headless_raw}" ]]; then
        notes+=("valor headless descoñecido \"${headless_raw}\", aplicando \"old\"")
      fi
      headless_mode="old"
      ;;
  esac

  if [[ "${headless_orixe}" == "config" && "${headless_lower}" != "old" && -n "${headless_raw}" ]]; then
    notes+=("configuración do ficheiro ignorada")
  fi

  export PUPPETEER_HEADLESS="${headless_mode}"
  if [[ -z "${PUPPETEER_HEADLESS_MODE:-}" ]]; then
    export PUPPETEER_HEADLESS_MODE="${headless_mode}"
  fi

  if [[ "${headless_mode}" == "shell" ]]; then
    notes+=("precisa dun servidor X")
  fi

  info_suffix=""
  if (( ${#notes[@]} > 0 )); then
    info_suffix=" (${notes[0]}"
    for ((i = 1; i < ${#notes[@]}; i++)); do
      info_suffix+=", ${notes[i]}"
    done
    info_suffix+=")"
  fi

  xvfb_run_bin="$(command -v xvfb-run || true)"
  manual_xvfb_bin="$(command -v Xvfb || true)"
  xvfb_pid=""

  cleanup() {
    if [[ -n "${xvfb_pid}" ]]; then
      kill "${xvfb_pid}" 2>/dev/null || true
      wait "${xvfb_pid}" 2>/dev/null || true
    fi
  }
  trap cleanup EXIT

  limpa_procs_chromium() {
    local timestamp
    timestamp="$(date '+%Y-%m-%d %H:%M:%S')"

    if ! command -v pgrep >/dev/null 2>&1; then
      echo "[$timestamp] WARN: A ferramenta pgrep non está dispoñible; omítese a limpeza de procesos Chromium." >> "${rexistro}"
      return
    fi

    local user_name
    user_name="$(id -un 2>/dev/null || true)"
    if [[ -z "${user_name}" ]]; then
      user_name="$(whoami 2>/dev/null || true)"
    fi

    if [[ -z "${user_name}" ]]; then
      echo "[$timestamp] WARN: Non se puido determinar o usuario actual; omítese a limpeza de procesos Chromium." >> "${rexistro}"
      return
    fi

    mapfile -t raw_pids < <(pgrep -u "${user_name}" -f '[c]hromium' 2>/dev/null || true)

    if (( ${#raw_pids[@]} == 0 )); then
      echo "[$timestamp] INFO: Ningún proceso Chromium activo antes de iniciar o crawler." >> "${rexistro}"
      return
    fi

    local -A pid_seen=()
    local -a pids=()
    local pid
    for pid in "${raw_pids[@]}"; do
      if [[ -n "${pid}" && -z "${pid_seen[$pid]:-}" ]]; then
        pid_seen[$pid]=1
        pids+=("${pid}")
      fi
    done

    local total="${#pids[@]}"
    echo "[$timestamp] INFO: Atopáronse ${total} procesos Chromium activos. Téntase detelos." >> "${rexistro}"
    echo "Atopáronse ${total} procesos de Chromium activos; tentando detelos..."

    local cmd
    for pid in "${pids[@]}"; do
      kill "${pid}" 2>/dev/null || true
    done

    local start_secs
    start_secs="$(date +%s)"
    local -a pendentes=("${pids[@]}")
    local -a restantes=()

    while true; do
      restantes=()
      for pid in "${pendentes[@]}"; do
        if kill -0 "${pid}" 2>/dev/null; then
          restantes+=("${pid}")
        fi
      done

      if (( ${#restantes[@]} == 0 )); then
        echo "[$(date '+%Y-%m-%d %H:%M:%S')] INFO: Todos os procesos Chromium se detiveron con SIGTERM." >> "${rexistro}"
        return
      fi

      if (( "$(date +%s)" - start_secs >= 5 )); then
        echo "[$(date '+%Y-%m-%d %H:%M:%S')] WARN: Persistencia de ${#restantes[@]} procesos Chromium; enviarase SIGKILL." >> "${rexistro}"
        for pid in "${restantes[@]}"; do
          kill -9 "${pid}" 2>/dev/null || true
        done

        local -a restantes_despois=()
        for pid in "${restantes[@]}"; do
          if kill -0 "${pid}" 2>/dev/null; then
            restantes_despois+=("${pid}")
          fi
        done

        if (( ${#restantes_despois[@]} > 0 )); then
          echo "[$(date '+%Y-%m-%d %H:%M:%S')] ERROR: Algúns procesos Chromium persisten malia os intentos: ${restantes_despois[*]}" >> "${rexistro}"
        else
          echo "[$(date '+%Y-%m-%d %H:%M:%S')] INFO: Os procesos Chromium restantes foron detidos con SIGKILL." >> "${rexistro}"
        fi
        return
      fi

      sleep 0.5
    done
  }

  limpa_procs_chromium

  if [[ -z "${DISPLAY:-}" && "${headless_mode}" == "shell" ]]; then
    if [[ -n "${xvfb_run_bin}" ]]; then
      echo "[$(date '+%Y-%m-%d %H:%M:%S')] INFO: Executando con xvfb-run para fornecer un DISPLAY virtual." >> "${rexistro}"
      "${xvfb_run_bin}" --auto-servernum --server-args="-screen 0 1920x1080x24 -ac" \
        "${node_bin}" "${script_crawler}"
      exit $?
    elif [[ -n "${manual_xvfb_bin}" ]]; then
      export DISPLAY=":99"
      echo "[$(date '+%Y-%m-%d %H:%M:%S')] INFO: Iniciando Xvfb manualmente en DISPLAY ${DISPLAY}." >> "${rexistro}"
      "${manual_xvfb_bin}" "${DISPLAY}" -screen 0 1920x1080x24 -ac &
      xvfb_pid=$!
      sleep 2
    else
      echo "[$(date '+%Y-%m-%d %H:%M:%S')] ERROR: Non se atopou ningún servidor X, instala 'xvfb' ou exporta DISPLAY." >> "${rexistro}"
      exit 3
    fi
  fi

  "${node_bin}" "${script_crawler}"
) >> "${rexistro}" 2>&1 &

pid=$!
pai_pid="$(ps -o ppid= -p "$$" 2>/dev/null | tr -d ' ' || true)"
pai_nome=""
if [[ -n "${pai_pid}" ]]; then
  pai_nome="$(ps -o comm= -p "${pai_pid}" 2>/dev/null | tr -d ' ' || true)"
fi
# Se estamos nun cron (PPID 1) ou solicitamos execución sincronizada, agardamos a saída
if [[ "${CRON_WAIT:-0}" == "1" || "${pai_pid}" == "1" || "${pai_nome}" == "cron" || "${pai_nome}" == "crond" || "${pai_nome}" == "systemd-cron" ]]; then
  wait "${pid}"
else
  echo "Proceso lanzado en segundo plano con PID ${pid}"
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] Proceso en segundo plano (PID ${pid})" >> "${rexistro}"
fi

