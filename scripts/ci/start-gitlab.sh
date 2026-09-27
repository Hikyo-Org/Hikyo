#!/usr/bin/env bash
# Start a disposable GitLab CE over HTTPS for the GitLab adapter end-to-end
# lifecycle (#159) and export HIKYO_TEST_GITLAB_* into $GITHUB_ENV.
#
# The instance serves a throwaway self-signed certificate for 127.0.0.1; the
# test trusts it through HIKYO_TEST_GITLAB_CA_FILE and pins its SPKI through
# HIKYO_TEST_GITLAB_SPKI_PIN, so the adapter's pinned egress path is the one
# exercised. The root personal token only bootstraps a group, a project, and a
# group access token: the adapter itself runs with the least-privilege bot
# token, so the default personal-token refusal stays on.
set -euo pipefail

image="gitlab/gitlab-ce:19.4.1-ce.0@sha256:9b33b45b9f42d176bada85ee5ecb81ddab7e506c435f44cd582206e284b2809c"
dir=$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/hikyo-gitlab.XXXXXX")
container="hikyo-gitlab-$$"
origin="https://127.0.0.1"

openssl req -x509 -newkey rsa:2048 -nodes -days 2 -subj "/CN=127.0.0.1" \
	-addext "subjectAltName=IP:127.0.0.1" \
	-keyout "$dir/127.0.0.1.key" -out "$dir/127.0.0.1.crt" >/dev/null 2>&1
chmod 0644 "$dir/127.0.0.1.key" "$dir/127.0.0.1.crt"
pin=$(openssl x509 -in "$dir/127.0.0.1.crt" -pubkey -noout |
	openssl pkey -pubin -outform der | openssl dgst -sha256 -binary | base64)

docker run -d --name "$container" --shm-size 256m -p 127.0.0.1:443:443 \
	-v "$dir:/etc/gitlab/ssl:ro" \
	-e GITLAB_OMNIBUS_CONFIG="external_url '$origin'; letsencrypt['enable'] = false; \
nginx['ssl_certificate'] = '/etc/gitlab/ssl/127.0.0.1.crt'; nginx['ssl_certificate_key'] = '/etc/gitlab/ssl/127.0.0.1.key'; \
prometheus_monitoring['enable'] = false; puma['worker_processes'] = 0; sidekiq['concurrency'] = 5; \
gitlab_rails['initial_root_password'] = 'hikyo-e2e-$(openssl rand -hex 12)';" \
	"$image" >/dev/null
echo "HIKYO_TEST_GITLAB_CONTAINER=$container" >>"${GITHUB_ENV:?GITHUB_ENV must be set}"

deadline=$((SECONDS + 1500))
# /-/readiness is limited to the monitoring allow-list, which excludes the
# Docker bridge; the sign-in page answers 200 once Rails serves requests.
until curl -fsS --cacert "$dir/127.0.0.1.crt" "$origin/users/sign_in" >/dev/null 2>&1; do
	if [ "$SECONDS" -ge "$deadline" ]; then
		docker logs --tail 200 "$container" >&2 || true
		echo "start-gitlab: GitLab did not become ready" >&2
		exit 1
	fi
	sleep 10
done

root_token="glpat-hikyo-e2e-root-$(openssl rand -hex 8)"
echo "::add-mask::$root_token"
docker exec "$container" gitlab-rails runner "
token = User.find_by_username('root').personal_access_tokens.build(scopes: ['api'], name: 'hikyo-e2e-bootstrap', expires_at: 2.days.from_now)
token.set_token('$root_token')
token.save!
" >/dev/null

api() {
	curl -fsS --cacert "$dir/127.0.0.1.crt" -H "PRIVATE-TOKEN: $root_token" -H 'Content-Type: application/json' "$@"
}
group_id=$(api -X POST "$origin/api/v4/groups" -d '{"name":"hikyo-e2e","path":"hikyo-e2e","visibility":"private"}' | jq -er '.id | select(type == "number" and . > 0 and . == floor)')
api -X POST "$origin/api/v4/projects" -d "{\"name\":\"app\",\"path\":\"app\",\"namespace_id\":$group_id,\"visibility\":\"private\"}" >/dev/null
expires=$(date -u -d '+2 days' +%F)
# Group CI/CD variables require the Owner role (50); project variables need
# Maintainer. One group token covers both destinations the lifecycle uses.
bot_token=$(api -X POST "$origin/api/v4/groups/$group_id/access_tokens" \
	-d "{\"name\":\"hikyo-e2e\",\"scopes\":[\"api\"],\"access_level\":50,\"expires_at\":\"$expires\"}" | jq -r .token)
if [ -z "$bot_token" ] || [ "$bot_token" = null ]; then
	echo "start-gitlab: group access token was not minted" >&2
	exit 1
fi
echo "::add-mask::$bot_token"

{
	echo "HIKYO_TEST_GITLAB_REQUIRED=1"
	echo "HIKYO_TEST_GITLAB_URL=$origin"
	echo "HIKYO_TEST_GITLAB_TOKEN=$bot_token"
	echo "HIKYO_TEST_GITLAB_PROJECT=hikyo-e2e/app"
	echo "HIKYO_TEST_GITLAB_GROUP=hikyo-e2e"
	echo "HIKYO_TEST_GITLAB_CA_FILE=$dir/127.0.0.1.crt"
	echo "HIKYO_TEST_GITLAB_SPKI_PIN=$pin"
	echo "HIKYO_TEST_GITLAB_ALLOWED_CIDR=127.0.0.0/8"
} >>"${GITHUB_ENV:?GITHUB_ENV must be set}"
