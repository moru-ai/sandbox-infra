ENV := $(shell cat ../.last_used_env || echo "not-set")
MORU_DEBUG := ""
-include ../.env.${ENV}

.PHONY: test
test:
	MORU_DOMAIN=$(DOMAIN_NAME) MORU_DEBUG=$(MORU_DEBUG) MORU_API_KEY=$(MORU_API_KEY) ./test_moru.sh
