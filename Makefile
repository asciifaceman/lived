.PHONY: help build build-embed run dev test db-setup db-recreate db-migrate db-verify db-set-admin frontend-install frontend-dev frontend-build frontend-build-embed

DEPRECATION_NOTICE = "[DEPRECATED] Makefile wrappers will be removed in a future release. Use 'mage <target>' directly."

define mage_forward
	@echo $(DEPRECATION_NOTICE)
	@mage $(1)
endef

help:
	$(call mage_forward,help)

build:
	$(call mage_forward,build)

build-embed:
	$(call mage_forward,buildEmbed)

run:
	$(call mage_forward,run)

dev:
	$(call mage_forward,dev)

test:
	$(call mage_forward,test)

db-setup:
	$(call mage_forward,dbSetup)

db-recreate:
	$(call mage_forward,dbRecreate)

db-migrate:
	$(call mage_forward,dbMigrate)

db-verify:
	$(call mage_forward,dbVerify)

db-set-admin:
	$(call mage_forward,dbSetAdmin)

frontend-install:
	$(call mage_forward,frontendInstall)

frontend-dev:
	$(call mage_forward,frontendDev)

frontend-build:
	$(call mage_forward,frontendBuild)

frontend-build-embed:
	$(call mage_forward,frontendBuildEmbed)
