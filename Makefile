.PHONY: help build build-embed run dev test db-setup db-recreate db-migrate db-verify frontend-install frontend-dev frontend-build frontend-build-embed

help:
	mage help

build:
	mage build

build-embed:
	mage buildEmbed

run:
	mage run

dev:
	mage dev

test:
	mage test

db-setup:
	mage dbSetup

db-recreate:
	mage dbRecreate

db-migrate:
	mage dbMigrate

db-verify:
	mage dbVerify

frontend-install:
	mage frontendInstall

frontend-dev:
	mage frontendDev

frontend-build:
	mage frontendBuild

frontend-build-embed:
	mage frontendBuildEmbed
