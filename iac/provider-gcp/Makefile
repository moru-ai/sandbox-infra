ENV := $(shell cat ../../.last_used_env || echo "not-set")
ENV_FILE := ../../.env.${ENV}

-include ${ENV_FILE}

TF := $(shell which terraform)
TERRAFORM_STATE_BUCKET ?= $(GCP_PROJECT_ID)-terraform-state
TEMPLATE_BUCKET_LOCATION ?= $(GCP_REGION)

# Set the terraform environment variable only if the environment variable is set
# Strip the passed variable name (it's space sensitive) and check if the variable is set, if yes return TF_VAR_<variable_name>=<value> with the variable name in lower case
define tfvar
$(if $(value $(strip $(1))), TF_VAR_$(shell echo $(strip $(1)) | tr A-Z a-z)=$($(strip $(1))))
endef

tf_vars := 	TF_VAR_environment=$(TERRAFORM_ENVIRONMENT) \
	$(call tfvar, CLIENT_MACHINE_TYPE) \
	$(call tfvar, CLIENT_CLUSTER_SIZE) \
	$(call tfvar, CLIENT_CLUSTER_SIZE_MAX) \
	$(call tfvar, CLIENT_CLUSTER_AUTOSCALING_CPU_TARGET) \
	$(call tfvar, CLIENT_CLUSTER_AUTOSCALING_MEMORY_TARGET) \
	$(call tfvar, CLIENT_CLUSTER_ROOT_DISK_SIZE_GB) \
	$(call tfvar, CLIENT_CLUSTER_CACHE_DISK_SIZE_GB) \
	$(call tfvar, CLIENT_CLUSTER_CACHE_DISK_COUNT) \
	$(call tfvar, API_MACHINE_TYPE) \
	$(call tfvar, API_CLUSTER_SIZE) \
	$(call tfvar, API_USE_NAT) \
	$(call tfvar, API_NAT_IPS) \
	$(call tfvar, BUILD_MACHINE_TYPE) \
	$(call tfvar, BUILD_CLUSTER_SIZE) \
	$(call tfvar, BUILD_CLUSTER_ROOT_DISK_SIZE_GB) \
	$(call tfvar, BUILD_CLUSTER_CACHE_DISK_SIZE_GB) \
	$(call tfvar, BUILD_CLUSTER_CACHE_DISK_COUNT) \
	$(call tfvar, SERVER_MACHINE_TYPE) \
	$(call tfvar, SERVER_CLUSTER_SIZE) \
	$(call tfvar, CLICKHOUSE_CLUSTER_SIZE) \
	$(call tfvar, CLICKHOUSE_MACHINE_TYPE) \
	$(call tfvar, LOKI_MACHINE_TYPE) \
	$(call tfvar, LOKI_CLUSTER_SIZE) \
	$(call tfvar, GCP_PROJECT_ID) \
	$(call tfvar, GCP_REGION) \
	$(call tfvar, GCP_ZONE) \
	$(call tfvar, DOMAIN_NAME) \
	$(call tfvar, ADDITIONAL_API_SERVICES_JSON) \
	$(call tfvar, PREFIX) \
	$(call tfvar, ALLOW_SANDBOX_INTERNET) \
	$(call tfvar, API_RESOURCES_CPU_COUNT) \
	$(call tfvar, API_RESOURCES_MEMORY_MB) \
	$(call tfvar, INGRESS_COUNT) \
	$(call tfvar, CLIENT_PROXY_COUNT) \
	$(call tfvar, CLIENT_PROXY_RESOURCES_CPU_COUNT) \
	$(call tfvar, CLIENT_PROXY_RESOURCES_MEMORY_MB) \
	$(call tfvar, CLICKHOUSE_RESOURCES_CPU_COUNT) \
	$(call tfvar, CLICKHOUSE_RESOURCES_MEMORY_MB) \
	$(call tfvar, CLIENT_PROXY_UPDATE_MAX_PARALLEL) \
	$(call tfvar, LOKI_RESOURCES_CPU_COUNT) \
	$(call tfvar, LOKI_RESOURCES_MEMORY_MB) \
	$(call tfvar, OTEL_TRACING_PRINT) \
	$(call tfvar, OTEL_COLLECTOR_RESOURCES_CPU_COUNT) \
	$(call tfvar, OTEL_COLLECTOR_RESOURCES_MEMORY_MB) \
	$(call tfvar, TEMPLATE_BUCKET_NAME) \
	$(call tfvar, TEMPLATE_BUCKET_LOCATION) \
	$(call tfvar, ENVD_TIMEOUT) \
	$(call tfvar, REDIS_MANAGED) \
	$(call tfvar, GRAFANA_MANAGED) \
	$(call tfvar, FILESTORE_CACHE_ENABLED) \
	$(call tfvar, FILESTORE_CACHE_TIER) \
	$(call tfvar, FILESTORE_CACHE_CAPACITY_GB) \
	$(call tfvar, FILESTORE_MAX_DISK_USAGE_TARGET) \
	$(call tfvar, BUILD_CLUSTER_CACHE_DISK_TYPE) \
	$(call tfvar, CLIENT_CLUSTER_CACHE_DISK_TYPE) \
	$(call tfvar, MIN_CPU_PLATFORM) \
	$(call tfvar, REMOTE_REPOSITORY_ENABLED) \
	$(call tfvar, CLIENT_BOOT_DISK_TYPE) \
	$(call tfvar, BUILD_BOOT_DISK_TYPE) \
	$(call tfvar, API_BOOT_DISK_TYPE) \
	$(call tfvar, SERVER_BOOT_DISK_TYPE) \
	$(call tfvar, CLICKHOUSE_BOOT_DISK_TYPE) \
	$(call tfvar, LOKI_BOOT_DISK_TYPE)

# Extra root-module resources that must exist before deploying Nomad jobs.
# `plan-without-jobs` uses Terraform `-target` to avoid applying Nomad jobs too early, but that also skips
# non-module resources in this directory (e.g. secrets in api.tf). These targets ensure Step 9 produces
# the secret containers you populate in Step 10.
PRE_JOBS_TARGETS := \
	-target=google_artifact_registry_repository.custom_environments_repository \
	-target=google_artifact_registry_repository_iam_member.custom_environments_repository_member \
	-target=google_secret_manager_secret.postgres_connection_string \
	-target=google_secret_manager_secret.supabase_jwt_secrets \
	-target=google_secret_manager_secret_version.supabase_jwt_secrets \
	-target=google_secret_manager_secret.redis_cluster_url \
	-target=google_secret_manager_secret_version.redis_cluster_url \
	-target=google_secret_manager_secret.posthog_api_key \
	-target=google_secret_manager_secret_version.posthog_api_key \
	-target=google_secret_manager_secret.api_secret \
	-target=google_secret_manager_secret_version.api_secret_value \
	-target=google_secret_manager_secret.api_admin_token \
	-target=google_secret_manager_secret_version.api_admin_token_value \
	-target=google_secret_manager_secret.sandbox_access_token_hash_seed \
	-target=google_secret_manager_secret_version.sandbox_access_token_hash_seed \
	-target=google_secret_manager_secret.edge_api_secret \
	-target=google_secret_manager_secret_version.edge_api_secret \
	-target=google_service_account.docker_registry_service_account \
	-target=google_artifact_registry_repository_iam_member.orchestration_repository_member \
	-target=google_service_account_key.google_service_key

.PHONY: init
init:
	@ printf "Initializing Terraform for env: `tput setaf 2``tput bold`$(ENV)`tput sgr0`\n\n"
	gcloud storage buckets create gs://$(TERRAFORM_STATE_BUCKET) --location $(GCP_REGION) --project $(GCP_PROJECT_ID) --default-storage-class STANDARD  --uniform-bucket-level-access > /dev/null 2>&1 || true

	# Enable object versioning (keeps deleted/replaced objects as older versions)
	gcloud storage buckets update gs://$(TERRAFORM_STATE_BUCKET) --versioning --soft-delete-duration=30d

	# Create a temporary file for lifecycle rules
	$(eval LIFECYCLE_FILE := $(shell mktemp))

	# Set lifecycle rules to delete non-live objects after 30 days or more than 50 newer versions
	echo '{"rule":[{"action":{"type":"Delete"},"condition":{"isLive":false,"age":30}},{"action":{"type":"Delete"},"condition":{"numNewerVersions":50}}]}' > $(LIFECYCLE_FILE)
	gcloud storage buckets update gs://$(TERRAFORM_STATE_BUCKET) --lifecycle-file=$(LIFECYCLE_FILE)

	# Remove the temporary lifecycle file
	@ rm -f $(LIFECYCLE_FILE)

	$(TF) init -input=false -reconfigure -backend-config=bucket=$(TERRAFORM_STATE_BUCKET)
	$(tf_vars) $(TF) apply -target=module.init -auto-approve -input=false -compact-warnings

	TERRAFORM_STATE_BUCKET="$(TERRAFORM_STATE_BUCKET)" $(MAKE) -C nomad-cluster-disk-image init build
	gcloud auth configure-docker "${GCP_REGION}-docker.pkg.dev" --quiet

.PHONY: switch
switch:
	@ printf "Switching Terraform backend for different env ${ENV}\n"
	$(TF) init -input=false -upgrade -reconfigure -backend-config=bucket=$(TERRAFORM_STATE_BUCKET)

.PHONY: destroy
destroy:
	@ printf "Destroying Terraform for env: `tput setaf 2``tput bold`$(ENV)`tput sgr0`\n\n"
	@ $(tf_vars) $(TF) destroy

.PHONY: plan
plan:
	@ printf "Planning Terraform for env: `tput setaf 2``tput bold`$(ENV)`tput sgr0`\n\n"
	@ $(TF) fmt -recursive
	@ $(tf_vars) $(TF) plan -out=.tfplan.$(ENV) -compact-warnings

# Deploy all jobs in Nomad
.PHONY: plan-only-jobs
plan-only-jobs:
	@ printf "Planning Terraform for env: `tput setaf 2``tput bold`$(ENV)`tput sgr0`\n\n"
	$(TF) fmt -recursive
	@ $(tf_vars) $(TF) plan -out=.tfplan.$(ENV) -compact-warnings -target=module.nomad

# Deploy a specific job name in Nomad
# When job name is specified, all '-' are replaced with '_' in the job name
.PHONY: plan-only-jobs/%
plan-only-jobs/%:
	@ printf "Planning Terraform for env: `tput setaf 2``tput bold`$(ENV)`tput sgr0`\n\n"
	$(TF) fmt -recursive
	@ $(tf_vars) $(TF) plan -out=.tfplan.$(ENV) -compact-warnings -target=module.nomad.nomad_job.$$(echo "$(notdir $@)" | tr '-' '_');

.PHONY: plan-without-jobs
plan-without-jobs:
	@ printf "Planning Terraform for env: `tput setaf 2``tput bold`$(ENV)`tput sgr0`\n\n"
	$(eval TARGET := $(shell cat main.tf | grep "^module" | awk '{print $$2}' | tr ' ' '\n' | grep -v -e "nomad" | awk '{print "-target=module." $$0 ""}' | xargs))
	$(tf_vars) \
	$(TF) plan \
	-out=.tfplan.$(ENV) \
	-input=false \
	-compact-warnings \
	-parallelism=20 \
	$(TARGET) \
	$(PRE_JOBS_TARGETS)

.PHONY: apply
apply:
	@ printf "Applying Terraform for env: `tput setaf 2``tput bold`$(ENV)`tput sgr0`\n\n"
	$(tf_vars) \
	$(TF) apply \
	-auto-approve \
	-input=false \
	-compact-warnings \
	-parallelism=20 \
	.tfplan.$(ENV)
	@ rm .tfplan.$(ENV)

# Shortcut to importing resources into Terraform state (e.g. after creating resources manually or switching between different branches for the same environment)
.PHONY: import
import:
	@ printf "Importing resources for env: `tput setaf 2``tput bold`$(ENV)`tput sgr0`\n\n"
	$(tf_vars) $(TF) import "$(TARGET)" "$(ID)" -no-color

# Login for Packer and Docker (uses gcloud user creds)
# Login for Terraform (uses application default creds)
.PHONY: provider-login
provider-login:
	gcloud --quiet auth login
	gcloud config set project "$(GCP_PROJECT_ID)"
	gcloud --quiet auth configure-docker "$(GCP_REGION)-docker.pkg.dev"
	gcloud --quiet auth application-default login
