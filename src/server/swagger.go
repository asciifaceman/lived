package server

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

const openAPISpecJSON = `{
  "openapi": "3.0.3",
  "info": {
    "title": "Lived API",
    "version": "v1",
    "description": "Server-authoritative API for the Lived incremental game."
  },
  "servers": [
    { "url": "/" }
  ],
  "tags": [
    {
      "name": "System",
      "description": "Save lifecycle and world status operations."
    },
    {
      "name": "Player",
      "description": "Player-facing save and progression status operations."
    },
    {
      "name": "Auth",
      "description": "Account authentication and session management endpoints."
    },
    {
      "name": "Onboarding",
      "description": "Character onboarding endpoints for authenticated accounts."
    },
    {
      "name": "Stream",
      "description": "UI-oriented live stream endpoints."
    },
    {
      "name": "Feed",
      "description": "Realm-scoped public world activity feed endpoints."
    },
    {
      "name": "Chat",
      "description": "Realm chat channels and messages."
    },
    {
      "name": "Admin",
      "description": "Admin control-plane and operational telemetry endpoints."
    },
    {
      "name": "MMO",
      "description": "Realm-scoped MMO telemetry and aggregate stats."
    }
  ],
  "paths": {
    "/health": {
      "get": {
        "summary": "Health check",
        "responses": {
          "200": {
            "description": "Service heartbeat",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          }
        }
      }
    },
    "/metrics": {
      "get": {
        "summary": "Prometheus metrics",
        "description": "Exposes operational metrics in Prometheus text format for scraping.",
        "responses": {
          "200": {
            "description": "Prometheus metrics payload"
          }
        }
      }
    },
    "/v1/system/export": {
      "get": {
        "tags": [
          "System"
        ],
        "summary": "Export save game",
        "description": "Exports the complete save as minified base64url JSON for re-ingestion. Disabled when MMO auth mode is enabled.",
        "responses": {
          "200": {
            "description": "Exported save payload",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "409": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "500": {
            "$ref": "#/components/responses/ErrorResponse"
          }
        }
      }
    },
    "/v1/system/import": {
      "post": {
        "tags": [
          "System"
        ],
        "summary": "Import save game",
        "description": "Replaces all stored game state with the supplied save payload. Disabled when MMO auth mode is enabled.",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "$ref": "#/components/schemas/ImportRequest"
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Save imported successfully",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "400": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "409": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "500": {
            "$ref": "#/components/responses/ErrorResponse"
          }
        }
      }
    },
    "/v1/system/new": {
      "post": {
        "tags": [
          "System"
        ],
        "summary": "Start new game",
        "description": "Re-bootstrap state to a new game with the provided initial player name. Disabled when MMO auth mode is enabled (use onboarding endpoints).",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "$ref": "#/components/schemas/NewGameRequest"
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "New game initialized",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "400": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "409": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "500": {
            "$ref": "#/components/responses/ErrorResponse"
          }
        }
      }
    },
    "/v1/system/status": {
      "get": {
        "tags": [
          "System"
        ],
        "summary": "Get world status",
        "description": "Returns world/runtime status. In MMO mode this endpoint requires bearer auth, resolves the authenticated account character (optional characterId), and omits legacy global save payload. Stat payloads are split into coreStats (trainable character attributes) and derivedStats (runtime values). Current formulas: max_stamina = 100 + endurance*3, stamina_recovery_rate = 8 + floor(endurance/4).",
        "parameters": [
          {
            "name": "characterId",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1
            },
            "description": "Optional character selector in MMO mode."
          }
        ],
        "security": [
          {
            "BearerAuth": []
          }
        ],
        "responses": {
          "200": {
            "description": "World status snapshot",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                },
                "examples": {
                  "statusSnapshot": {
                    "summary": "System status with split stats",
                    "value": {
                      "status": "success",
                      "message": "The world turns, and its state is known.",
                      "data": {
                        "simulationTick": 842,
                        "players": ["Wanderer"],
                        "inventory": {
                          "coins": 124,
                          "wood": 8
                        },
                        "coreStats": {
                          "strength": 6,
                          "social": 3,
                          "endurance": 10
                        },
                        "derivedStats": {
                          "stamina": 97,
                          "max_stamina": 130,
                          "stamina_recovery_rate": 10
                        },
                        "stats": {
                          "strength": 6,
                          "social": 3,
                          "endurance": 10,
                          "stamina": 97,
                          "max_stamina": 130,
                          "stamina_recovery_rate": 10
                        }
                      }
                    }
                  }
                }
              }
            }
          },
          "400": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "401": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "404": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "500": {
            "$ref": "#/components/responses/ErrorResponse"
          }
        }
      }
    },
    "/v1/system/version": {
      "get": {
        "tags": [
          "System"
        ],
        "summary": "Get API and build versions",
        "description": "Returns API, backend, and frontend version metadata for clients and UIs.",
        "responses": {
          "200": {
            "description": "Version metadata",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          }
        }
      }
    },
    "/v1/auth/register": {
      "post": {
        "tags": ["Auth"],
        "summary": "Register account",
        "description": "Creates account and returns access/refresh tokens.",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "$ref": "#/components/schemas/RegisterRequest"
              }
            }
          }
        },
        "responses": {
          "201": {
            "description": "Account registered",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "400": { "$ref": "#/components/responses/ErrorResponse" },
          "409": { "$ref": "#/components/responses/ErrorResponse" },
          "500": { "$ref": "#/components/responses/ErrorResponse" }
        }
      }
    },
    "/v1/auth/login": {
      "post": {
        "tags": ["Auth"],
        "summary": "Login",
        "description": "Authenticates account credentials and returns access/refresh tokens.",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "$ref": "#/components/schemas/LoginRequest"
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Login successful",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "400": { "$ref": "#/components/responses/ErrorResponse" },
          "401": { "$ref": "#/components/responses/ErrorResponse" },
          "500": { "$ref": "#/components/responses/ErrorResponse" }
        }
      }
    },
    "/v1/auth/refresh": {
      "post": {
        "tags": ["Auth"],
        "summary": "Refresh session",
        "description": "Rotates refresh session and returns a new access/refresh pair.",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "$ref": "#/components/schemas/RefreshRequest"
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Session refreshed",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "400": { "$ref": "#/components/responses/ErrorResponse" },
          "401": { "$ref": "#/components/responses/ErrorResponse" },
          "500": { "$ref": "#/components/responses/ErrorResponse" }
        }
      }
    },
    "/v1/auth/logout": {
      "post": {
        "tags": ["Auth"],
        "summary": "Logout",
        "description": "Revokes current authenticated session.",
        "security": [
          { "BearerAuth": [] }
        ],
        "responses": {
          "200": {
            "description": "Session revoked",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "401": { "$ref": "#/components/responses/ErrorResponse" },
          "500": { "$ref": "#/components/responses/ErrorResponse" }
        }
      }
    },
    "/v1/auth/me": {
      "get": {
        "tags": ["Auth"],
        "summary": "Get account context",
        "description": "Returns authenticated account identity, roles, and linked characters.",
        "security": [
          { "BearerAuth": [] }
        ],
        "responses": {
          "200": {
            "description": "Account context",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "401": { "$ref": "#/components/responses/ErrorResponse" },
          "500": { "$ref": "#/components/responses/ErrorResponse" }
        }
      }
    },
    "/v1/onboarding/start": {
      "post": {
        "tags": ["Onboarding"],
        "summary": "Start onboarding",
        "description": "Creates initial character for authenticated account in selected realm. Idempotent per account+realm.",
        "security": [
          { "BearerAuth": [] }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "$ref": "#/components/schemas/OnboardingStartRequest"
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Already onboarded for realm",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "201": {
            "description": "Onboarding completed",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "400": { "$ref": "#/components/responses/ErrorResponse" },
          "401": { "$ref": "#/components/responses/ErrorResponse" },
          "409": { "$ref": "#/components/responses/ErrorResponse" },
          "500": { "$ref": "#/components/responses/ErrorResponse" }
        }
      }
    },
    "/v1/onboarding/status": {
      "get": {
        "tags": ["Onboarding"],
        "summary": "Get onboarding status",
        "description": "Returns onboarding status and characters for authenticated account.",
        "security": [
          { "BearerAuth": [] }
        ],
        "responses": {
          "200": {
            "description": "Onboarding status",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "401": { "$ref": "#/components/responses/ErrorResponse" },
          "500": { "$ref": "#/components/responses/ErrorResponse" }
        }
      }
    },
    "/v1/player/status": {
      "get": {
        "tags": [
          "Player"
        ],
        "summary": "Get player save status",
        "description": "Returns player-facing status. In MMO mode this requires bearer auth and resolves the authenticated account character (optional characterId selector). Stats are split into coreStats (directly trained attributes like strength/social/endurance) and derivedStats (computed runtime values such as stamina, max_stamina, and stamina_recovery_rate). Current formulas: max_stamina = 100 + endurance*3, stamina_recovery_rate = 8 + floor(endurance/4).",
        "parameters": [
          {
            "name": "characterId",
            "in": "query",
            "required": false,
            "schema": { "type": "integer", "minimum": 1 },
            "description": "Optional character selector in MMO mode."
          },
          {
            "name": "Idempotency-Key",
            "in": "header",
            "required": false,
            "schema": { "type": "string", "maxLength": 128 },
            "description": "Optional idempotency key for safe retries when idempotency middleware is enabled."
          }
        ],
        "security": [
          { "BearerAuth": [] }
        ],
        "responses": {
          "200": {
            "description": "Player save status snapshot",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                },
                "examples": {
                  "playerSnapshot": {
                    "summary": "Player status with trainable vs derived stats",
                    "value": {
                      "status": "success",
                      "message": "Player save status loaded.",
                      "data": {
                        "hasPrimaryPlayer": true,
                        "playerName": "Wanderer",
                        "simulationTick": 842,
                        "inventory": {
                          "coins": 124,
                          "wood": 8
                        },
                        "coreStats": {
                          "strength": 6,
                          "social": 3,
                          "endurance": 10
                        },
                        "derivedStats": {
                          "stamina": 97,
                          "max_stamina": 130,
                          "stamina_recovery_rate": 10
                        },
                        "stats": {
                          "strength": 6,
                          "social": 3,
                          "endurance": 10,
                          "stamina": 97,
                          "max_stamina": 130,
                          "stamina_recovery_rate": 10
                        }
                      }
                    }
                  }
                }
              }
            }
          },
          "400": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "401": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "404": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "409": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "500": {
            "$ref": "#/components/responses/ErrorResponse"
          }
        }
      }
    },
    "/v1/player/inventory": {
      "get": {
        "tags": [
          "Player"
        ],
        "summary": "Get player inventory",
        "description": "Returns player inventory status. In MMO mode this requires bearer auth and resolves the authenticated account character (optional characterId selector).",
        "parameters": [
          {
            "name": "characterId",
            "in": "query",
            "required": false,
            "schema": { "type": "integer", "minimum": 1 },
            "description": "Optional character selector in MMO mode."
          }
        ],
        "security": [
          { "BearerAuth": [] }
        ],
        "responses": {
          "200": {
            "description": "Player inventory snapshot",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "400": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "401": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "404": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "500": {
            "$ref": "#/components/responses/ErrorResponse"
          }
        }
      }
    },
    "/v1/player/behaviors": {
      "get": {
        "tags": [
          "Player"
        ],
        "summary": "Get player behaviors",
        "description": "Returns player behavior queue/history. In MMO mode this requires bearer auth and resolves the authenticated account character (optional characterId selector).",
        "parameters": [
          {
            "name": "characterId",
            "in": "query",
            "required": false,
            "schema": { "type": "integer", "minimum": 1 },
            "description": "Optional character selector in MMO mode."
          }
        ],
        "security": [
          { "BearerAuth": [] }
        ],
        "responses": {
          "200": {
            "description": "Player behaviors snapshot",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "400": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "401": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "404": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "500": {
            "$ref": "#/components/responses/ErrorResponse"
          }
        }
      }
    },
    "/v1/stream/world": {
      "get": {
        "tags": [
          "Stream"
        ],
        "summary": "Stream world snapshots",
        "description": "WebSocket endpoint for UI live updates. Clients should upgrade the connection and receive continuous world/player snapshots including tick, clock, dayPart, market session, and player summary. In MMO mode this route requires bearer auth, resolves stream context by authenticated account character (optional characterId selector), and enforces configured concurrent connection limits per account/session.",
        "parameters": [
          {
            "name": "characterId",
            "in": "query",
            "required": false,
            "schema": { "type": "integer", "minimum": 1 },
            "description": "Optional character selector in MMO mode."
          }
        ],
        "security": [
          { "BearerAuth": [] }
        ],
        "responses": {
          "101": {
            "description": "Switching Protocols (WebSocket upgrade successful)"
          },
          "400": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "401": {
            "$ref": "#/components/responses/ErrorResponse"
          }
        }
      }
    },
    "/v1/system/behaviors/start": {
      "post": {
        "tags": [
          "System"
        ],
        "summary": "Queue player behavior",
        "description": "Queues a player behavior to be processed by the world loop. In MMO mode this requires bearer auth and resolves the authenticated account character (optional characterId selector). For market-open-required behaviors, optional marketWait (e.g. 12h, 2d) controls how long it waits for market open before failing. Scheduling mode defaults to 'once' and supports 'repeat' or 'repeat-until' ('repeatUntil' duration required for 'repeat-until'). Successful response data includes behaviorKey, behaviorName, and resolved scheduling mode metadata. When idempotency is enabled and Idempotency-Key is provided, responses include Idempotency-Status: stored|replayed.",
        "parameters": [
          {
            "name": "characterId",
            "in": "query",
            "required": false,
            "schema": { "type": "integer", "minimum": 1 },
            "description": "Optional character selector in MMO mode."
          }
        ],
        "security": [
          { "BearerAuth": [] }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "$ref": "#/components/schemas/StartBehaviorRequest"
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Behavior queued",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "400": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "401": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "404": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "500": {
            "$ref": "#/components/responses/ErrorResponse"
          }
        }
      }
    },
    "/v1/system/behaviors/catalog": {
      "get": {
        "tags": [
          "System"
        ],
        "summary": "List player behaviors",
        "description": "Returns the player-accessible behavior catalog. World/AI behaviors are not exposed here. In MMO mode this requires bearer auth and evaluates availability for the authenticated account character (optional characterId selector). Catalog rows include key plus display metadata (name/label/summary), queue/availability flags, and human-readable unavailableReason requirement hints.",
        "parameters": [
          {
            "name": "characterId",
            "in": "query",
            "required": false,
            "schema": { "type": "integer", "minimum": 1 },
            "description": "Optional character selector in MMO mode."
          }
        ],
        "security": [
          { "BearerAuth": [] }
        ],
        "responses": {
          "200": {
            "description": "Behavior catalog",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "400": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "401": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "500": {
            "$ref": "#/components/responses/ErrorResponse"
          }
        }
      }
    },
    "/v1/system/market/status": {
      "get": {
        "tags": ["System"],
        "summary": "Get market ticker status",
        "description": "Returns ticker-style market data including session open/close state. In MMO auth mode this endpoint requires bearer auth and resolves realm from the authenticated account character (optional characterId).",
        "parameters": [
          {
            "name": "realmId",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1,
              "default": 1
            }
          },
          {
            "name": "characterId",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1
            },
            "description": "Optional character selector in MMO auth mode."
          }
        ],
        "security": [
          { "BearerAuth": [] }
        ],
        "responses": {
          "200": {
            "description": "Market ticker snapshot",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "400": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "401": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "403": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "500": {
            "$ref": "#/components/responses/ErrorResponse"
          }
        }
      }
    },
    "/v1/system/market/history": {
      "get": {
        "tags": ["System"],
        "summary": "Get market history",
        "description": "Returns market history entries with tick/price/delta/source. In MMO auth mode this endpoint requires bearer auth and resolves realm from the authenticated account character (optional characterId).",
        "parameters": [
          {
            "name": "symbol",
            "in": "query",
            "required": false,
            "schema": {
              "type": "string"
            }
          },
          {
            "name": "limit",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1,
              "maximum": 500,
              "default": 100
            }
          },
          {
            "name": "realmId",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1,
              "default": 1
            }
          },
          {
            "name": "characterId",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1
            },
            "description": "Optional character selector in MMO auth mode."
          }
        ],
        "security": [
          { "BearerAuth": [] }
        ],
        "responses": {
          "200": {
            "description": "Market history",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "400": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "401": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "403": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "500": {
            "$ref": "#/components/responses/ErrorResponse"
          }
        }
      }
    },
    "/v1/system/ascend": {
      "post": {
        "tags": [
          "System"
        ],
        "summary": "Ascend to next run",
        "description": "Resets run-state and grants permanent meta bonuses. In MMO mode this endpoint is authenticated and applies character-scoped ascension reset within the selected character realm.",
        "parameters": [
          {
            "name": "characterId",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1
            },
            "description": "Optional character selector in MMO mode."
          }
        ],
        "security": [
          { "BearerAuth": [] }
        ],
        "requestBody": {
          "required": false,
          "content": {
            "application/json": {
              "schema": {
                "$ref": "#/components/schemas/AscendRequest"
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Ascension completed",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "400": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "401": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "409": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "500": {
            "$ref": "#/components/responses/ErrorResponse"
          }
        }
      }
    },
    "/v1/mmo/stats/system": {
      "get": {
        "tags": ["MMO"],
        "summary": "Get realm system stats",
        "description": "Returns realm-scoped world tick and behavior/event aggregates. In MMO auth mode this endpoint requires bearer auth and resolves realm from the authenticated account character (optional characterId).",
        "parameters": [
          {
            "name": "realmId",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1,
              "default": 1
            }
          },
          {
            "name": "characterId",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1
            },
            "description": "Optional character selector in MMO auth mode."
          }
        ],
        "security": [
          { "BearerAuth": [] }
        ],
        "responses": {
          "200": {
            "description": "MMO system stats",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "400": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "401": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "403": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "500": {
            "$ref": "#/components/responses/ErrorResponse"
          }
        }
      }
    },
    "/v1/mmo/stats/players": {
      "get": {
        "tags": ["MMO"],
        "summary": "Get realm player stats",
        "description": "Returns realm-scoped active character/account/player and currency aggregates. In MMO auth mode this endpoint requires bearer auth and resolves realm from the authenticated account character (optional characterId).",
        "parameters": [
          {
            "name": "realmId",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1,
              "default": 1
            }
          },
          {
            "name": "characterId",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1
            },
            "description": "Optional character selector in MMO auth mode."
          }
        ],
        "security": [
          { "BearerAuth": [] }
        ],
        "responses": {
          "200": {
            "description": "MMO player stats",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "400": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "401": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "403": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "500": {
            "$ref": "#/components/responses/ErrorResponse"
          }
        }
      }
    },
    "/v1/mmo/stats/economy": {
      "get": {
        "tags": ["MMO"],
        "summary": "Get realm economy stats",
        "description": "Returns realm-scoped market ticker rows and aggregated player inventory quantities. In MMO auth mode this endpoint requires bearer auth and resolves realm from the authenticated account character (optional characterId).",
        "parameters": [
          {
            "name": "realmId",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1,
              "default": 1
            }
          },
          {
            "name": "characterId",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1
            },
            "description": "Optional character selector in MMO auth mode."
          }
        ],
        "security": [
          { "BearerAuth": [] }
        ],
        "responses": {
          "200": {
            "description": "MMO economy stats",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "400": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "401": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "403": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "500": {
            "$ref": "#/components/responses/ErrorResponse"
          }
        }
      }
    },
    "/v1/feed/public": {
      "get": {
        "tags": ["Feed"],
        "summary": "Get public world feed",
        "description": "Returns realm-scoped public world events in reverse chronological order. In MMO auth mode this endpoint requires bearer auth and resolves realm from the authenticated account character (optional characterId).",
        "parameters": [
          {
            "name": "realmId",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1,
              "default": 1
            }
          },
          {
            "name": "characterId",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1
            },
            "description": "Optional character selector in MMO auth mode."
          },
          {
            "name": "limit",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1,
              "maximum": 200,
              "default": 50
            }
          }
        ],
        "security": [
          { "BearerAuth": [] }
        ],
        "responses": {
          "200": {
            "description": "Public feed events",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "400": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "401": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "403": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "500": {
            "$ref": "#/components/responses/ErrorResponse"
          }
        }
      }
    },
    "/v1/chat/channels": {
      "get": {
        "tags": ["Chat"],
        "summary": "Get chat channels",
        "description": "Returns active chat channels for the resolved realm. Each channel includes key, name, optional subject, description, and binding metadata ('scope', 'scopeKey') where v1 channels are realm-bound ('scope=realm', 'scopeKey=realm:{id}'). In MMO auth mode realm resolves from authenticated character context.",
        "responses": {
          "200": {
            "description": "Chat channels",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          }
        }
      }
    },
    "/v1/chat/messages": {
      "get": {
        "tags": ["Chat"],
        "summary": "Get chat messages",
        "description": "Returns realm/channel chat messages. Response includes channel binding metadata ('scope', 'scopeKey') and message rows with messageClass (player|moderator|admin|system), authorRole, authorBadges, and censorship metadata (censored/censorHits) for UI differentiation. In MMO auth mode this endpoint requires bearer auth and resolves realm from the authenticated account character (optional characterId).",
        "parameters": [
          {
            "name": "realmId",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1,
              "default": 1
            }
          },
          {
            "name": "characterId",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1
            },
            "description": "Optional character selector in MMO auth mode."
          },
          {
            "name": "channel",
            "in": "query",
            "required": false,
            "schema": {
              "type": "string",
              "default": "global"
            }
          },
          {
            "name": "limit",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1,
              "maximum": 200,
              "default": 100
            }
          }
        ],
        "security": [
          { "BearerAuth": [] }
        ],
        "responses": {
          "200": {
            "description": "Chat messages",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "400": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "401": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "403": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "500": {
            "$ref": "#/components/responses/ErrorResponse"
          }
        }
      },
      "post": {
        "tags": ["Chat"],
        "summary": "Post chat message",
        "description": "Posts a public chat message. In MMO mode this endpoint requires bearer auth and posts in the selected character realm. Active global chat wordlist rules (applied across all realms/channels) may redact matching content and return a 200 success with censorship metadata; uncensored posts return 201. When idempotency is enabled and Idempotency-Key is provided, responses include Idempotency-Status: stored|replayed.",
        "parameters": [
          {
            "name": "characterId",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1
            },
            "description": "Optional character selector in MMO mode."
          }
        ],
        "security": [
          { "BearerAuth": [] }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["message"],
                "properties": {
                  "message": { "type": "string", "maxLength": 280 },
                  "channel": { "type": "string", "default": "global" }
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Chat message posted with censorship",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "201": {
            "description": "Chat message posted",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "400": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "401": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "500": {
            "$ref": "#/components/responses/ErrorResponse"
          }
        }
      }
    },
    "/v1/admin/chat/channels": {
      "post": {
        "tags": ["Admin"],
        "summary": "Create or update chat channel",
        "description": "Creates a realm-bound chat channel instance or updates metadata for an existing one, including optional subject text used for channel purpose/context. Explicit binding scope is supported via 'scope' and currently only 'realm' is allowed. Responses include 'scopeKey=realm:{id}' for stable binding identity. Requires admin role.",
        "security": [
          { "BearerAuth": [] }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["key", "name"],
                "properties": {
                  "scope": { "type": "string", "enum": ["realm"], "default": "realm" },
                  "realmId": { "type": "integer", "minimum": 1, "default": 1 },
                  "key": { "type": "string", "maxLength": 32 },
                  "name": { "type": "string", "minLength": 2, "maxLength": 64 },
                  "subject": { "type": "string", "maxLength": 140 },
                  "description": { "type": "string", "maxLength": 255 }
                }
              }
            }
          }
        },
        "responses": {
          "201": {
            "description": "Channel created or updated",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "400": { "$ref": "#/components/responses/ErrorResponse" },
          "401": { "$ref": "#/components/responses/ErrorResponse" },
          "403": { "$ref": "#/components/responses/ErrorResponse" },
          "500": { "$ref": "#/components/responses/ErrorResponse" }
        }
      }
    },
    "/v1/admin/chat/channels/{key}": {
      "delete": {
        "tags": ["Admin"],
        "summary": "Disable chat channel",
        "description": "Marks a chat channel inactive (channel key remains reserved). Explicit binding scope is supported via query params and currently only 'scope=realm' is allowed. Responses include 'scopeKey=realm:{id}' for stable binding identity. Requires admin role.",
        "security": [
          { "BearerAuth": [] }
        ],
        "parameters": [
          {
            "name": "key",
            "in": "path",
            "required": true,
            "schema": { "type": "string" }
          },
          {
            "name": "scope",
            "in": "query",
            "required": false,
            "schema": { "type": "string", "enum": ["realm"], "default": "realm" }
          },
          {
            "name": "realmId",
            "in": "query",
            "required": false,
            "schema": { "type": "integer", "minimum": 1, "default": 1 }
          }
        ],
        "responses": {
          "200": {
            "description": "Channel disabled",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "400": { "$ref": "#/components/responses/ErrorResponse" },
          "401": { "$ref": "#/components/responses/ErrorResponse" },
          "403": { "$ref": "#/components/responses/ErrorResponse" },
          "404": { "$ref": "#/components/responses/ErrorResponse" },
          "500": { "$ref": "#/components/responses/ErrorResponse" }
        }
      }
    },
    "/v1/admin/chat/channels/{key}/flush": {
      "post": {
        "tags": ["Admin"],
        "summary": "Flush channel messages",
        "description": "Deletes persisted chat events for the selected realm-bound channel instance. Explicit binding scope is supported via 'scope' and currently only 'realm' is allowed. Responses include 'scopeKey=realm:{id}' for stable binding identity. Requires admin role and reasonCode.",
        "security": [
          { "BearerAuth": [] }
        ],
        "parameters": [
          {
            "name": "key",
            "in": "path",
            "required": true,
            "schema": { "type": "string" }
          }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["reasonCode"],
                "properties": {
                  "scope": { "type": "string", "enum": ["realm"], "default": "realm" },
                  "realmId": { "type": "integer", "minimum": 1, "default": 1 },
                  "reasonCode": { "type": "string", "minLength": 3, "maxLength": 64 },
                  "note": { "type": "string", "maxLength": 500 }
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Channel flushed",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "400": { "$ref": "#/components/responses/ErrorResponse" },
          "401": { "$ref": "#/components/responses/ErrorResponse" },
          "403": { "$ref": "#/components/responses/ErrorResponse" },
          "404": { "$ref": "#/components/responses/ErrorResponse" },
          "500": { "$ref": "#/components/responses/ErrorResponse" }
        }
      }
    },
    "/v1/admin/chat/channels/{key}/moderation": {
      "post": {
        "tags": ["Admin"],
        "summary": "Apply channel moderation",
        "description": "Applies channel-level ban, unban, or kick for a target account in a realm-bound channel instance. Explicit binding scope is supported via 'scope' and currently only 'realm' is allowed. Responses include 'scopeKey=realm:{id}' for stable binding identity. Requires admin role and reasonCode.",
        "security": [
          { "BearerAuth": [] }
        ],
        "parameters": [
          {
            "name": "key",
            "in": "path",
            "required": true,
            "schema": { "type": "string" }
          }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["accountId", "action", "reasonCode"],
                "properties": {
                  "scope": { "type": "string", "enum": ["realm"], "default": "realm" },
                  "realmId": { "type": "integer", "minimum": 1, "default": 1 },
                  "accountId": { "type": "integer", "minimum": 1 },
                  "action": { "type": "string", "enum": ["ban", "unban", "kick"] },
                  "durationMinutes": { "type": "integer", "minimum": 1 },
                  "reasonCode": { "type": "string", "minLength": 3, "maxLength": 64 },
                  "note": { "type": "string", "maxLength": 500 }
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Moderation applied",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "400": { "$ref": "#/components/responses/ErrorResponse" },
          "401": { "$ref": "#/components/responses/ErrorResponse" },
          "403": { "$ref": "#/components/responses/ErrorResponse" },
          "404": { "$ref": "#/components/responses/ErrorResponse" },
          "500": { "$ref": "#/components/responses/ErrorResponse" }
        }
      }
    },
    "/v1/admin/chat/channels/{key}/system-message": {
      "post": {
        "tags": ["Admin"],
        "summary": "Publish system chat message",
        "description": "Publishes a system-class message into a realm-bound channel instance and records immutable admin audit context. Explicit binding scope is supported via 'scope' and currently only 'realm' is allowed. Responses include 'scopeKey=realm:{id}' for stable binding identity. Requires admin role and reasonCode.",
        "security": [
          { "BearerAuth": [] }
        ],
        "parameters": [
          {
            "name": "key",
            "in": "path",
            "required": true,
            "schema": { "type": "string" }
          }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["message", "reasonCode"],
                "properties": {
                  "scope": { "type": "string", "enum": ["realm"], "default": "realm" },
                  "realmId": { "type": "integer", "minimum": 1, "default": 1 },
                  "message": { "type": "string", "minLength": 1, "maxLength": 280 },
                  "reasonCode": { "type": "string", "minLength": 3, "maxLength": 64 },
                  "note": { "type": "string", "maxLength": 500 }
                }
              }
            }
          }
        },
        "responses": {
          "201": {
            "description": "System message published",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "400": { "$ref": "#/components/responses/ErrorResponse" },
          "401": { "$ref": "#/components/responses/ErrorResponse" },
          "403": { "$ref": "#/components/responses/ErrorResponse" },
          "404": { "$ref": "#/components/responses/ErrorResponse" },
          "500": { "$ref": "#/components/responses/ErrorResponse" }
        }
      }
    },
    "/v1/admin/chat/wordlist": {
      "get": {
        "tags": ["Admin"],
        "summary": "List chat wordlist rules",
        "description": "Lists active unsafe-word rules for the global chat policy scope ('scope=global') that applies across all realms/channels. Responses include 'policyScopeKey=global' for stable policy binding identity. Requires admin role.",
        "security": [
          { "BearerAuth": [] }
        ],
        "parameters": [],
        "responses": {
          "200": {
            "description": "Wordlist rules",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/APIResponse" }
              }
            }
          },
          "400": { "$ref": "#/components/responses/ErrorResponse" },
          "401": { "$ref": "#/components/responses/ErrorResponse" },
          "403": { "$ref": "#/components/responses/ErrorResponse" },
          "500": { "$ref": "#/components/responses/ErrorResponse" }
        }
      },
      "post": {
        "tags": ["Admin"],
        "summary": "Add chat wordlist rule",
        "description": "Adds or reactivates a global chat policy wordlist rule ('scope=global') that applies across all realms/channels. Responses include 'policyScopeKey=global' for stable policy binding identity. Current v1 mode supports contains matching. Requires admin role.",
        "security": [
          { "BearerAuth": [] }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["term", "reasonCode"],
                "properties": {
                  "scope": { "type": "string", "enum": ["global"], "default": "global" },
                  "term": { "type": "string", "minLength": 2, "maxLength": 128 },
                  "matchMode": { "type": "string", "enum": ["contains"], "default": "contains" },
                  "reasonCode": { "type": "string", "minLength": 3, "maxLength": 64 },
                  "note": { "type": "string", "maxLength": 500 }
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Wordlist rule saved",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/APIResponse" }
              }
            }
          },
          "400": { "$ref": "#/components/responses/ErrorResponse" },
          "401": { "$ref": "#/components/responses/ErrorResponse" },
          "403": { "$ref": "#/components/responses/ErrorResponse" },
          "500": { "$ref": "#/components/responses/ErrorResponse" }
        }
      }
    },
    "/v1/admin/chat/wordlist/{ruleId}": {
      "delete": {
        "tags": ["Admin"],
        "summary": "Remove chat wordlist rule",
        "description": "Deactivates a global chat policy wordlist rule ('scope=global') that applies across all realms/channels. Responses include 'policyScopeKey=global' for stable policy binding identity. Requires admin role.",
        "security": [
          { "BearerAuth": [] }
        ],
        "parameters": [
          {
            "name": "ruleId",
            "in": "path",
            "required": true,
            "schema": { "type": "integer", "minimum": 1 }
          }
        ],
        "responses": {
          "200": {
            "description": "Wordlist rule removed",
            "content": {
              "application/json": {
                "schema": { "$ref": "#/components/schemas/APIResponse" }
              }
            }
          },
          "400": { "$ref": "#/components/responses/ErrorResponse" },
          "401": { "$ref": "#/components/responses/ErrorResponse" },
          "403": { "$ref": "#/components/responses/ErrorResponse" },
          "404": { "$ref": "#/components/responses/ErrorResponse" },
          "500": { "$ref": "#/components/responses/ErrorResponse" }
        }
      }
    },
    "/v1/admin/realms": {
      "get": {
        "tags": ["Admin"],
        "summary": "List known realms",
        "description": "Returns discovered realm IDs and active character counts. Requires admin role.",
        "security": [
          { "BearerAuth": [] }
        ],
        "responses": {
          "200": {
            "description": "Realm list",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "401": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "403": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "500": {
            "$ref": "#/components/responses/ErrorResponse"
          }
        }
      }
    },
    "/v1/admin/audit": {
      "get": {
        "tags": ["Admin"],
        "summary": "List admin audit entries",
        "description": "Returns immutable admin audit entries with optional filters, including actor username search. Requires admin role.",
        "security": [
          { "BearerAuth": [] }
        ],
        "parameters": [
          {
            "name": "realmId",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1
            }
          },
          {
            "name": "actorAccountId",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1
            }
          },
          {
            "name": "actorUsername",
            "in": "query",
            "required": false,
            "schema": {
              "type": "string",
              "maxLength": 64
            }
          },
          {
            "name": "actionKey",
            "in": "query",
            "required": false,
            "schema": {
              "type": "string",
              "maxLength": 64
            }
          },
          {
            "name": "beforeId",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1
            },
            "description": "Cursor for older entries (returns entries with id < beforeId)."
          },
          {
            "name": "includeRawJson",
            "in": "query",
            "required": false,
            "schema": {
              "type": "boolean",
              "default": false
            },
            "description": "When true, includes decoded before/after payloads for each row."
          },
          {
            "name": "limit",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1,
              "maximum": 500,
              "default": 100
            }
          }
        ],
        "responses": {
          "200": {
            "description": "Audit entries",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "400": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "401": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "403": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "500": {
            "$ref": "#/components/responses/ErrorResponse"
          }
        }
      }
    },
    "/v1/admin/audit/{id}": {
      "get": {
        "tags": ["Admin"],
        "summary": "Get admin audit entry",
        "description": "Returns a single admin audit entry by ID with decoded before/after payloads. Requires admin role.",
        "security": [
          { "BearerAuth": [] }
        ],
        "parameters": [
          {
            "name": "id",
            "in": "path",
            "required": true,
            "schema": {
              "type": "integer",
              "minimum": 1
            },
            "description": "Admin audit entry ID"
          },
          {
            "name": "includeRawJson",
            "in": "query",
            "required": false,
            "schema": {
              "type": "boolean",
              "default": false
            },
            "description": "When true, includes decoded before/after payloads."
          }
        ],
        "responses": {
          "200": {
            "description": "Audit entry",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "400": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "401": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "403": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "404": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "500": {
            "$ref": "#/components/responses/ErrorResponse"
          }
        }
      }
    },
    "/v1/admin/audit/export": {
      "get": {
        "tags": ["Admin"],
        "summary": "Export admin audit entries as CSV",
        "description": "Exports immutable admin audit entries as CSV with optional filters, including actor username search. Requires admin role.",
        "security": [
          { "BearerAuth": [] }
        ],
        "parameters": [
          {
            "name": "realmId",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1
            }
          },
          {
            "name": "actorAccountId",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1
            }
          },
          {
            "name": "actorUsername",
            "in": "query",
            "required": false,
            "schema": {
              "type": "string",
              "maxLength": 64
            }
          },
          {
            "name": "actionKey",
            "in": "query",
            "required": false,
            "schema": {
              "type": "string",
              "maxLength": 64
            }
          },
          {
            "name": "beforeId",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1
            }
          },
          {
            "name": "limit",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1,
              "maximum": 500,
              "default": 100
            }
          }
        ],
        "responses": {
          "200": {
            "description": "Audit CSV export",
            "content": {
              "text/csv": {
                "schema": {
                  "type": "string",
                  "format": "binary"
                }
              }
            }
          },
          "400": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "401": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "403": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "500": {
            "$ref": "#/components/responses/ErrorResponse"
          }
        }
      }
    },
    "/v1/admin/stats": {
      "get": {
        "tags": ["Admin"],
        "summary": "Get admin stats",
        "description": "Returns global MMO operational counters and recent admin-audit aggregates. Requires admin role.",
        "security": [
          { "BearerAuth": [] }
        ],
        "parameters": [
          {
            "name": "windowTicks",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1,
              "maximum": 43200,
              "default": 1440
            },
            "description": "Tick window used for admin audit by-action/by-realm aggregates."
          }
        ],
        "responses": {
          "200": {
            "description": "Admin stats",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "401": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "403": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "500": {
            "$ref": "#/components/responses/ErrorResponse"
          }
        }
      }
    },
    "/v1/admin/realms/{id}/actions": {
      "post": {
        "tags": ["Admin"],
        "summary": "Apply realm admin action",
        "description": "Applies an admin action to the selected realm and records an immutable audit event. Supports market and maintenance actions. Requires admin role.",
        "security": [
          { "BearerAuth": [] }
        ],
        "parameters": [
          {
            "name": "id",
            "in": "path",
            "required": true,
            "schema": {
              "type": "integer",
              "minimum": 1
            },
            "description": "Realm ID"
          }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["action", "reasonCode"],
                "properties": {
                  "action": {
                    "type": "string",
                    "enum": ["market_reset_defaults", "market_set_price", "realm_create", "realm_pause", "realm_resume"]
                  },
                  "reasonCode": {
                    "type": "string",
                    "maxLength": 64
                  },
                  "note": {
                    "type": "string",
                    "maxLength": 500
                  },
                  "itemKey": {
                    "type": "string"
                  },
                  "price": {
                    "type": "integer",
                    "minimum": 1
                  }
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Action applied",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "400": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "401": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "403": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "500": {
            "$ref": "#/components/responses/ErrorResponse"
          }
        }
      }
    },
    "/v1/admin/moderation/accounts/{id}/lock": {
      "post": {
        "tags": ["Admin"],
        "summary": "Lock account",
        "description": "Locks an account and revokes active sessions. Requires admin role. Returns conflict if targeting your own account or removing the last active admin account.",
        "security": [
          { "BearerAuth": [] }
        ],
        "parameters": [
          {
            "name": "id",
            "in": "path",
            "required": true,
            "schema": {
              "type": "integer",
              "minimum": 1
            },
            "description": "Account ID"
          }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["reasonCode"],
                "properties": {
                  "reasonCode": { "type": "string", "maxLength": 64 },
                  "note": { "type": "string", "maxLength": 500 }
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Account locked",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "400": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "401": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "403": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "409": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "404": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "500": {
            "$ref": "#/components/responses/ErrorResponse"
          }
        }
      }
    },
    "/v1/admin/moderation/accounts/{id}/unlock": {
      "post": {
        "tags": ["Admin"],
        "summary": "Unlock account",
        "description": "Unlocks an account. Requires admin role.",
        "security": [
          { "BearerAuth": [] }
        ],
        "parameters": [
          {
            "name": "id",
            "in": "path",
            "required": true,
            "schema": {
              "type": "integer",
              "minimum": 1
            },
            "description": "Account ID"
          }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["reasonCode"],
                "properties": {
                  "reasonCode": { "type": "string", "maxLength": 64 },
                  "note": { "type": "string", "maxLength": 500 }
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Account unlocked",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "400": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "401": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "403": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "404": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "500": {
            "$ref": "#/components/responses/ErrorResponse"
          }
        }
      }
    },
    "/v1/admin/moderation/accounts/{id}/roles": {
      "post": {
        "tags": ["Admin"],
        "summary": "Grant or revoke account role",
        "description": "Grants or revokes a role for an account. Requires admin role. Returns conflict when trying to revoke your own admin role or remove the last active admin account.",
        "security": [
          { "BearerAuth": [] }
        ],
        "parameters": [
          {
            "name": "id",
            "in": "path",
            "required": true,
            "schema": {
              "type": "integer",
              "minimum": 1
            },
            "description": "Account ID"
          }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["roleKey", "action", "reasonCode"],
                "properties": {
                  "roleKey": { "type": "string", "maxLength": 32 },
                  "action": { "type": "string", "enum": ["grant", "revoke"] },
                  "reasonCode": { "type": "string", "maxLength": 64 },
                  "note": { "type": "string", "maxLength": 500 }
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Role moderation applied",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "400": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "401": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "403": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "409": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "404": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "500": {
            "$ref": "#/components/responses/ErrorResponse"
          }
        }
      }
    },
    "/v1/admin/moderation/accounts/{id}/status": {
      "post": {
        "tags": ["Admin"],
        "summary": "Set account status",
        "description": "Sets account status for corrective or operational action. Requires admin role. Returns conflict when locking your own account or removing the last active admin account.",
        "security": [
          { "BearerAuth": [] }
        ],
        "parameters": [
          {
            "name": "id",
            "in": "path",
            "required": true,
            "schema": {
              "type": "integer",
              "minimum": 1
            },
            "description": "Account ID"
          }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["status", "reasonCode"],
                "properties": {
                  "status": { "type": "string", "enum": ["active", "locked"] },
                  "reasonCode": { "type": "string", "maxLength": 64 },
                  "note": { "type": "string", "maxLength": 500 },
                  "revokeSessions": { "type": "boolean" }
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Account status updated",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "400": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "401": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "403": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "409": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "404": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "500": {
            "$ref": "#/components/responses/ErrorResponse"
          }
        }
      }
    },
    "/v1/admin/moderation/characters": {
      "get": {
        "tags": ["Admin"],
        "summary": "List characters for moderation",
        "description": "Searches characters for administrative and corrective actions, including account username search. Requires admin role.",
        "security": [
          { "BearerAuth": [] }
        ],
        "parameters": [
          {
            "name": "accountId",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1
            }
          },
          {
            "name": "accountUsername",
            "in": "query",
            "required": false,
            "schema": {
              "type": "string",
              "maxLength": 64
            }
          },
          {
            "name": "realmId",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1
            }
          },
          {
            "name": "status",
            "in": "query",
            "required": false,
            "schema": {
              "type": "string",
              "enum": ["active", "locked"]
            }
          },
          {
            "name": "nameLike",
            "in": "query",
            "required": false,
            "schema": {
              "type": "string",
              "maxLength": 64
            }
          },
          {
            "name": "beforeId",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1
            }
          },
          {
            "name": "limit",
            "in": "query",
            "required": false,
            "schema": {
              "type": "integer",
              "minimum": 1,
              "maximum": 500,
              "default": 100
            }
          }
        ],
        "responses": {
          "200": {
            "description": "Character moderation list",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "400": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "401": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "403": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "500": {
            "$ref": "#/components/responses/ErrorResponse"
          }
        }
      }
    },
    "/v1/admin/moderation/characters/{id}": {
      "post": {
        "tags": ["Admin"],
        "summary": "Moderate character",
        "description": "Applies corrective character updates (name/status/primary flag). Requires admin role.",
        "security": [
          { "BearerAuth": [] }
        ],
        "parameters": [
          {
            "name": "id",
            "in": "path",
            "required": true,
            "schema": {
              "type": "integer",
              "minimum": 1
            },
            "description": "Character ID"
          }
        ],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "required": ["reasonCode"],
                "properties": {
                  "name": { "type": "string", "minLength": 3, "maxLength": 64 },
                  "status": { "type": "string", "enum": ["active", "locked"] },
                  "isPrimary": { "type": "boolean" },
                  "reasonCode": { "type": "string", "maxLength": 64 },
                  "note": { "type": "string", "maxLength": 500 }
                }
              }
            }
          }
        },
        "responses": {
          "200": {
            "description": "Character moderation applied",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
          },
          "400": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "401": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "403": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "404": {
            "$ref": "#/components/responses/ErrorResponse"
          },
          "500": {
            "$ref": "#/components/responses/ErrorResponse"
          }
        }
      }
    }
  },
  "components": {
    "schemas": {
      "APIResponse": {
        "type": "object",
        "required": ["status", "message"],
        "properties": {
          "status": {
            "type": "string",
            "enum": ["success", "error"]
          },
          "message": {
            "type": "string"
          },
          "requestId": {
            "type": "string"
          },
          "data": {
            "type": "object",
            "nullable": true
          }
        }
      },
      "ExportResponse": {
        "type": "object",
        "required": [
          "save"
        ],
        "properties": {
          "save": {
            "type": "string",
            "description": "Base64url minified save blob",
            "example": "eyJ2IjoxLCJwIjpbIlBsYXllciJdLCJ0IjowfQ"
          }
        }
      },
      "ImportRequest": {
        "type": "object",
        "required": [
          "save"
        ],
        "properties": {
          "save": {
            "type": "string",
            "description": "Base64url minified save blob generated by export"
          }
        }
      },
      "NewGameRequest": {
        "type": "object",
        "required": [
          "name"
        ],
        "properties": {
          "name": {
            "type": "string",
            "minLength": 1,
            "description": "Initial player name"
          }
        }
      },
      "StartBehaviorRequest": {
        "type": "object",
        "required": [
          "behaviorKey"
        ],
        "properties": {
          "behaviorKey": {
            "type": "string",
            "description": "Behavior key from the behavior catalog"
          },
          "marketWait": {
            "type": "string",
            "description": "Optional market wait timeout for market-open-required behaviors. Supports m/h/d units, for example 90m, 12h, 2d."
          },
          "mode": {
            "type": "string",
            "enum": ["once", "repeat", "repeat-until"],
            "default": "once",
            "description": "Optional scheduling mode. 'once' queues a single run, 'repeat' schedules continuous reruns, and 'repeat-until' reruns until 'repeatUntil' elapses."
          },
          "repeatUntil": {
            "type": "string",
            "description": "Required when mode='repeat-until'. Duration until repeat scheduling stops, supports m/h/d units (for example 2h, 1d)."
          }
        }
      },
      "AscendRequest": {
        "type": "object",
        "properties": {
          "name": {
            "type": "string",
            "description": "Optional new run player name"
          }
        }
      },
      "RegisterRequest": {
        "type": "object",
        "required": ["username", "password"],
        "properties": {
          "username": {
            "type": "string",
            "minLength": 3
          },
          "password": {
            "type": "string",
            "minLength": 8
          }
        }
      },
      "LoginRequest": {
        "type": "object",
        "required": ["username", "password"],
        "properties": {
          "username": {
            "type": "string"
          },
          "password": {
            "type": "string"
          }
        }
      },
      "RefreshRequest": {
        "type": "object",
        "required": ["refreshToken"],
        "properties": {
          "refreshToken": {
            "type": "string"
          }
        }
      },
      "OnboardingStartRequest": {
        "type": "object",
        "required": ["name"],
        "properties": {
          "name": {
            "type": "string",
            "minLength": 3,
            "maxLength": 64
          },
          "realmId": {
            "type": "integer",
            "minimum": 1,
            "default": 1
          }
        }
      },
      "ErrorResponse": {
        "type": "object",
        "properties": {
          "status": { "type": "string", "example": "error" },
          "message": { "type": "string" },
          "requestId": { "type": "string" }
        }
      }
    },
    "securitySchemes": {
      "BearerAuth": {
        "type": "http",
        "scheme": "bearer",
        "bearerFormat": "JWT"
      }
    },
    "responses": {
      "ErrorResponse": {
        "description": "Error response",
        "content": {
          "application/json": {
            "schema": {
              "$ref": "#/components/schemas/ErrorResponse"
            }
          }
        }
      }
    }
  }
}`

const swaggerUIHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>Lived API Docs</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css" />
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    window.ui = SwaggerUIBundle({
      url: '/swagger/openapi.json',
      dom_id: '#swagger-ui',
      deepLinking: true,
      presets: [SwaggerUIBundle.presets.apis],
      layout: 'BaseLayout'
    });
  </script>
</body>
</html>`

func swaggerUIRedirectHandler(c echo.Context) error {
	return c.Redirect(http.StatusPermanentRedirect, "/swagger/")
}

func swaggerUIHandler(c echo.Context) error {
	return c.HTML(http.StatusOK, swaggerUIHTML)
}

func swaggerSpecHandler(c echo.Context) error {
	return c.Blob(http.StatusOK, echo.MIMEApplicationJSONCharsetUTF8, []byte(openAPISpecJSON))
}
