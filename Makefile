.PHONY: help build build-embed run dev test db-setup db-recreate frontend-install frontend-dev frontend-build frontend-build-embed

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

frontend-install:
	mage frontendInstall

frontend-dev:
	mage frontendDev

frontend-build:
	mage frontendBuild

frontend-build-embed:
	mage frontendBuildEmbed
