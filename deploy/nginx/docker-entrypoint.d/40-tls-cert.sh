#!/bin/sh
# Prepara el certificado TLS antes de que arranque nginx (RNF-12).
#
# - Si ya hay un certificado y una clave en TLS_CERT_FILE y TLS_KEY_FILE
#   (p. ej. un volumen de solo lectura con el del laboratorio), los usa tal
#   cual y no toca nada.
# - Si no, genera uno autofirmado (ECDSA P-256) en esas mismas rutas, dentro
#   del contenedor. La clave nace aquí, en cada contenedor nuevo: nunca va en
#   la imagen publicada ni en el repositorio.
#
# Variables:
#   TLS_CERT_FILE, TLS_KEY_FILE  rutas (por defecto /etc/nginx/tls/tls.{crt,key})
#   TLS_EXTRA_SANS               nombres o IP adicionales, separados por comas
#                                (p. ej. 10.68.17.1,labbd-01); se suman a
#                                localhost, nginx y 127.0.0.1
#   TLS_DAYS                     vigencia en días (825 por defecto)
set -eu

cert="${TLS_CERT_FILE:-/etc/nginx/tls/tls.crt}"
key="${TLS_KEY_FILE:-/etc/nginx/tls/tls.key}"
days="${TLS_DAYS:-825}"
marker="$(dirname "$cert")/.self-signed"
renew_before=2592000 # 30 días, en segundos

log() { echo "40-tls-cert: $*"; }

if [ -s "$cert" ] && [ -s "$key" ]; then
  # Un certificado generado aquí se renueva si le quedan menos de 30 días
  # (el contenedor puede reiniciarse sin recrearse); uno montado nunca se toca.
  if [ ! -e "$marker" ]; then
    log "usando el certificado existente en $cert"
    exit 0
  fi
  if openssl x509 -in "$cert" -noout -checkend "$renew_before" >/dev/null 2>&1; then
    log "usando el certificado autofirmado ya generado en $cert"
    exit 0
  fi
  log "el certificado autofirmado vence pronto: se genera uno nuevo"
fi

# Lista de SAN: los fijos y los de TLS_EXTRA_SANS. Cada entrada con forma de
# IP (dígitos y puntos, o con «:») va como IP; el resto, como nombre DNS.
sans="DNS:localhost,DNS:nginx,IP:127.0.0.1"
extra="$(printf '%s' "${TLS_EXTRA_SANS:-}" | tr -d ' ')"
old_ifs="$IFS"
IFS=','
for entry in $extra; do
  [ -n "$entry" ] || continue
  case "$entry" in
    *:*) sans="$sans,IP:$entry" ;;
    *[!0-9.]*) sans="$sans,DNS:$entry" ;;
    *) sans="$sans,IP:$entry" ;;
  esac
done
IFS="$old_ifs"

umask 077
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes \
  -days "$days" \
  -subj "/CN=hydrovalley-proxy/O=hydrovalley" \
  -addext "subjectAltName=$sans" \
  -addext "extendedKeyUsage=serverAuth" \
  -keyout "$key" -out "$cert" 2>/dev/null
: >"$marker"
log "certificado autofirmado generado en $cert (SAN: $sans; $days días)"
