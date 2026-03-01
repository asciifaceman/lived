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
      "name": "Stream",
      "description": "UI-oriented live stream endpoints."
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
    "/v1/system/export": {
      "get": {
        "tags": [
          "System"
        ],
        "summary": "Export save game",
        "description": "Exports the complete save as minified base64url JSON for re-ingestion.",
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
        "description": "Replaces all stored game state with the supplied save payload.",
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
        "description": "Re-bootstrap state to a new game with the provided initial player name.",
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
        "description": "Returns current game status including players, inventory, stats, world age, version metadata, and exportable save blob.",
        "responses": {
          "200": {
            "description": "World status snapshot",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
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
    "/v1/player/status": {
      "get": {
        "tags": [
          "Player"
        ],
        "summary": "Get player save status",
        "description": "Returns player-facing save status including version metadata, encoded save blob, primary player profile, inventory, stats, behavior queue history, and ascension meta.",
        "responses": {
          "200": {
            "description": "Player save status snapshot",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/APIResponse"
                }
              }
            }
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
        "description": "Returns player-facing inventory status for the primary player.",
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
        "description": "Returns player behavior queue/history view for the primary player.",
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
        "description": "WebSocket endpoint for UI live updates. Clients should upgrade the connection and receive continuous world/player snapshots including tick, clock, dayPart, market session, and player summary.",
        "responses": {
          "101": {
            "description": "Switching Protocols (WebSocket upgrade successful)"
          },
          "400": {
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
        "description": "Queues a player behavior to be processed by the world loop. For market-open-required behaviors, optional marketWait (e.g. 12h, 2d) controls how long it waits for market open before failing.",
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
        "description": "Returns the player-accessible behavior catalog. World/AI behaviors are not exposed here.",
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
        "description": "Returns ticker-style market data including session open/close state.",
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
        "description": "Returns market history entries with tick/price/delta/source.",
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
          }
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
        "description": "Resets run-state and grants permanent meta bonuses.",
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
      "ErrorResponse": {
        "type": "object",
        "properties": {
          "status": { "type": "string", "example": "error" },
          "message": { "type": "string" },
          "requestId": { "type": "string" }
        }
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
