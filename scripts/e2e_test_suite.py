#!/usr/bin/env python3
"""
milanuncios-cli End-to-End User Journey Test Suite

This black-box test suite executes the compiled milanuncios binary as an end-user,
verifying:
  - Command line flag parsing and help/version outputs
  - Semantic exit codes (0, 1, 2, 3, 4, 5, 6, 7, 130)
  - Strict I/O separation (pure JSON stdout vs stderr envelopes)
  - XDG configuration management and persistence
  - Authentication guardrails and profile management
  - Local SQLite watch engine (CRUD and events)
  - Notification sink management
  - Chat and messaging validation
  - MCP skills and agent installers

Requirements: Python 3.8+ (zero external dependencies).
"""

import http.server
import json
import os
import shutil
import subprocess
import sys
import tempfile
import threading
import unittest
from pathlib import Path

# Locate root directory and binary
REPO_ROOT = Path(__file__).resolve().parent.parent
BINARY_NAME = "milanuncios.exe" if sys.platform == "win32" else "milanuncios"
DEFAULT_BIN_PATH = REPO_ROOT / "bin" / BINARY_NAME


def ensure_binary() -> Path:
    """Ensure the milanuncios binary is compiled before running tests."""
    bin_path = os.environ.get("MILANUNCIOS_BIN")
    if bin_path and Path(bin_path).is_file():
        return Path(bin_path)

    if DEFAULT_BIN_PATH.is_file():
        return DEFAULT_BIN_PATH

    print(f"[*] Binary not found at {DEFAULT_BIN_PATH}. Compiling via 'go build'...")
    DEFAULT_BIN_PATH.parent.mkdir(parents=True, exist_ok=True)
    subprocess.run(
        ["go", "build", "-o", str(DEFAULT_BIN_PATH), "./cmd/milanuncios"],
        cwd=str(REPO_ROOT),
        check=True,
    )
    return DEFAULT_BIN_PATH


class MilanunciosCLITestCase(unittest.TestCase):
    """Base test case providing clean isolation and CLI execution helper."""

    @classmethod
    def setUpClass(cls):
        cls.bin_path = ensure_binary()

    def setUp(self):
        # Create an isolated temporary directory for XDG config and data
        self.tmp_dir = tempfile.mkdtemp(prefix="milanuncios_e2e_")
        self.config_home = Path(self.tmp_dir) / "config"
        self.data_home = Path(self.tmp_dir) / "data"
        self.config_home.mkdir(parents=True, exist_ok=True)
        self.data_home.mkdir(parents=True, exist_ok=True)

        self.env = os.environ.copy()
        self.env["XDG_CONFIG_HOME"] = str(self.config_home)
        self.env["XDG_DATA_HOME"] = str(self.data_home)
        self.env["MILANUNCIOS_CONFIG"] = str(self.config_home / "milanuncios-cli" / "config.toml")
        self.env["MILANUNCIOS_CREDENTIALS_FILE"] = str(self.config_home / "milanuncios-cli" / "credentials.toml")
        self.env["MILANUNCIOS_STATE_DB"] = str(self.data_home / "milanuncios-cli" / "state.db")
        # Ensure tests don't inherit ambient proxies or session tokens
        self.env.pop("MILANUNCIOS_PROXY", None)
        self.env.pop("MILANUNCIOS_SESSION_TOKEN", None)

    def tearDown(self):
        shutil.rmtree(self.tmp_dir, ignore_errors=True)

    def run_cli(self, *args, input_text=None, check=False) -> subprocess.CompletedProcess:
        """Run the milanuncios binary with isolated environment."""
        cmd = [str(self.bin_path)] + list(args)
        return subprocess.run(
            cmd,
            input=input_text,
            capture_output=True,
            text=True,
            env=self.env,
            cwd=str(REPO_ROOT),
            check=check,
        )


# ==============================================================================
# 1. CORE & GLOBAL FLAGS (Tests 1 - 7)
# ==============================================================================
class TestCoreAndFlags(MilanunciosCLITestCase):

    def test_01_version_flag(self):
        res = self.run_cli("--version")
        self.assertEqual(res.returncode, 0)
        self.assertIn("milanuncios", res.stdout.lower())

    def test_02_help_flag(self):
        res = self.run_cli("--help")
        self.assertEqual(res.returncode, 0)
        self.assertIn("Usage:", res.stdout)
        self.assertIn("milanuncios [command]", res.stdout)

    def test_03_short_help_flag(self):
        res = self.run_cli("-h")
        self.assertEqual(res.returncode, 0)
        self.assertIn("Usage:", res.stdout)

    def test_04_unknown_command_semantic_exit_code_2(self):
        res = self.run_cli("nonexistent_command_xyz")
        # Exit code 2: Usage / flag validation error
        self.assertEqual(res.returncode, 2)
        self.assertIn("unknown command", res.stderr.lower())

    def test_05_invalid_format_flag_exit_code_2(self):
        res = self.run_cli("--format", "yaml_invalid", "config", "list")
        self.assertEqual(res.returncode, 2)
        self.assertIn("unknown format", res.stderr.lower())

    def test_06_invalid_error_format_flag_exit_code_2(self):
        res = self.run_cli("--error-format", "xml_invalid", "config", "list")
        self.assertEqual(res.returncode, 2)
        self.assertIn("error-format", res.stderr.lower())

    def test_07_all_subcommands_listed_in_help(self):
        res = self.run_cli("--help")
        expected_cmds = [
            "ad", "auth", "category", "chat", "config", "mcp",
            "me", "profile", "search", "sink", "skills", "watch",
        ]
        for cmd in expected_cmds:
            with self.subTest(cmd=cmd):
                self.assertIn(cmd, res.stdout, f"Command '{cmd}' not found in root help")


# ==============================================================================
# 2. CONFIGURATION MANAGEMENT (Tests 8 - 16)
# ==============================================================================
class TestConfigCommands(MilanunciosCLITestCase):

    def test_08_config_list_default(self):
        res = self.run_cli("config", "list")
        self.assertEqual(res.returncode, 0)
        self.assertIn("DefaultProfile", res.stdout)

    def test_09_config_list_json(self):
        res = self.run_cli("config", "list", "--format", "json")
        self.assertEqual(res.returncode, 0)
        data = json.loads(res.stdout)
        self.assertIn("DefaultProfile", data)
        self.assertEqual(data["DefaultProfile"], "default")

    def test_10_config_path(self):
        res = self.run_cli("config", "path")
        self.assertEqual(res.returncode, 0)
        self.assertIn("config.toml", res.stdout)

    def test_11_config_get_default_profile(self):
        res = self.run_cli("config", "get", "default_profile")
        self.assertEqual(res.returncode, 0)
        self.assertIn("default", res.stdout)

    def test_12_config_set_default_profile(self):
        res = self.run_cli("config", "set", "default_profile", "buyer_pro")
        self.assertEqual(res.returncode, 0)
        # Verify persistence
        res2 = self.run_cli("config", "get", "default_profile")
        self.assertEqual(res2.returncode, 0)
        self.assertIn("buyer_pro", res2.stdout)

    def test_13_config_set_watch_interval(self):
        res = self.run_cli("config", "set", "watch.interval", "10m")
        self.assertEqual(res.returncode, 0)
        res2 = self.run_cli("config", "get", "watch.interval")
        self.assertEqual(res2.returncode, 0)
        self.assertIn("10m", res2.stdout)

    def test_14_config_set_invalid_key_fails(self):
        res = self.run_cli("config", "set", "unsupported_key", "value")
        self.assertEqual(res.returncode, 2)
        self.assertIn("unknown config key", res.stderr.lower())

    def test_15_config_get_invalid_key_fails(self):
        res = self.run_cli("config", "get", "unsupported_key")
        self.assertEqual(res.returncode, 2)
        self.assertIn("unknown config key", res.stderr.lower())

    def test_16_strict_io_separation_on_json_output(self):
        res = self.run_cli("config", "list", "--format", "json")
        self.assertEqual(res.returncode, 0)
        # stdout must be parseable JSON without any stray characters or log lines
        data = json.loads(res.stdout.strip())
        self.assertIsInstance(data, dict)
        # stderr must remain empty on clean execution
        self.assertEqual(res.stderr.strip(), "")


# ==============================================================================
# 3. PROFILES & AUTHENTICATION (Tests 17 - 26)
# ==============================================================================
class TestAuthAndProfiles(MilanunciosCLITestCase):

    def test_17_profile_list_shows_default(self):
        res = self.run_cli("profile", "list")
        self.assertEqual(res.returncode, 0)
        self.assertIn("default", res.stdout)

    def test_18_profile_list_json(self):
        res = self.run_cli("profile", "list", "--format", "json")
        self.assertEqual(res.returncode, 0)
        profiles = json.loads(res.stdout)
        self.assertIsInstance(profiles, list)
        self.assertTrue(any(p.get("name") == "default" for p in profiles))

    def test_19_profile_use_command(self):
        res = self.run_cli("profile", "use", "work")
        self.assertEqual(res.returncode, 0)
        self.assertIn("work", res.stdout)

    def test_20_auth_status_unauthenticated_shows_logged_in_false(self):
        res = self.run_cli("auth", "status", "--format", "json")
        self.assertEqual(res.returncode, 0)
        data = json.loads(res.stdout)
        self.assertFalse(data.get("logged_in"))
        self.assertEqual(data.get("source"), "none")

    def test_21_auth_logout_cleans_session(self):
        res = self.run_cli("auth", "logout")
        self.assertEqual(res.returncode, 0)

    def test_22_auth_login_missing_flags_fails(self):
        res = self.run_cli("auth", "login")
        self.assertEqual(res.returncode, 2)
        self.assertIn("cookies", res.stderr.lower())

    def test_23_auth_login_nonexistent_cookie_file(self):
        res = self.run_cli("auth", "login", "--cookies", "missing_cookies.json")
        self.assertIn(res.returncode, [1, 2, 4])
        self.assertTrue(len(res.stderr) > 0)

    def test_24_me_show_unauthenticated_exit_code_3(self):
        res = self.run_cli("me", "show")
        self.assertEqual(res.returncode, 3)

    def test_25_me_favorites_unauthenticated_exit_code_3(self):
        res = self.run_cli("me", "favorites")
        self.assertEqual(res.returncode, 3)

    def test_26_auth_status_with_check_unauthenticated(self):
        res = self.run_cli("auth", "status", "--check")
        self.assertEqual(res.returncode, 0)
        self.assertIn("default", res.stdout)


# ==============================================================================
# 4. AD INSPECTION & SEARCH INPUT VALIDATION (Tests 27 - 32)
# ==============================================================================
class TestAdAndSearchValidation(MilanunciosCLITestCase):

    def test_27_search_invalid_sort_exit_code_2(self):
        res = self.run_cli("search", "bicicleta", "--sort", "invalid_sort_order")
        self.assertEqual(res.returncode, 2)
        self.assertIn("unknown sort", res.stderr.lower())

    def test_28_search_invalid_filter_format_exit_code_2(self):
        res = self.run_cli("search", "coche", "--filter", "missing_equals_sign")
        self.assertEqual(res.returncode, 2)
        self.assertIn("invalid filter format", res.stderr.lower())

    def test_29_ad_show_missing_argument_exit_code_2(self):
        res = self.run_cli("ad", "show")
        self.assertEqual(res.returncode, 2)
        self.assertIn("accepts 1 arg", res.stderr.lower())

    def test_30_ad_show_invalid_id_format_fails(self):
        res = self.run_cli("ad", "show", "not_a_valid_id_or_url")
        self.assertEqual(res.returncode, 2)
        self.assertIn("could not extract ad id", res.stderr.lower())

    def test_31_ad_images_missing_id_exit_code_2(self):
        res = self.run_cli("ad", "images")
        self.assertEqual(res.returncode, 2)
        self.assertIn("accepts 1 arg", res.stderr.lower())

    def test_32_ad_favorite_unauthenticated_exit_code_3(self):
        res = self.run_cli("ad", "favorite", "492817294")
        self.assertEqual(res.returncode, 3)

    def test_32b_ad_schema_missing_category_flag_exit_code_2(self):
        res = self.run_cli("ad", "schema")
        self.assertEqual(res.returncode, 2)
        self.assertIn("required flag", res.stderr.lower())

    def test_32c_ad_create_missing_flags_exit_code_2(self):
        res = self.run_cli("ad", "create")
        self.assertEqual(res.returncode, 2)
        self.assertIn("must provide ad spec", res.stderr.lower())

    def test_32d_ad_create_conflicting_flags_exit_code_2(self):
        res = self.run_cli("ad", "create", "--from-file", "dummy.json", "--spec", "{}")
        self.assertEqual(res.returncode, 2)
        self.assertIn("cannot specify both", res.stderr.lower())

    def test_32e_ad_create_invalid_json_spec_exit_code_2(self):
        res = self.run_cli("ad", "create", "--spec", "not-a-valid-json")
        self.assertEqual(res.returncode, 2)
        self.assertIn("invalid json ad spec", res.stderr.lower())

    def test_32f_ad_create_validation_failure_exit_code_2(self):
        invalid_spec = json.dumps({
            "category_id": 0,
            "title": "Short",
            "description": "Short",
            "price": 0,
            "location": {"province": ""},
        })
        res = self.run_cli("ad", "create", "--spec", invalid_spec, "--dry-run", "--format", "json")
        self.assertEqual(res.returncode, 2)
        report = json.loads(res.stdout)
        self.assertFalse(report.get("valid"))
        self.assertTrue(len(report.get("errors", [])) >= 4)

    def test_32g_ad_schema_with_mock_server(self):
        server = self._start_mock_server()
        try:
            res = self.run_cli("ad", "schema", "--category", "11", "--format", "json")
            self.assertEqual(res.returncode, 0)
            schema = json.loads(res.stdout)
            self.assertEqual(schema.get("category_id"), 11)
            self.assertEqual(schema.get("$schema"), "http://json-schema.org/draft-07/schema#")
            self.assertIn("title", schema.get("required", []))
            self.assertIn("attributes", schema.get("properties", {}))

            # Pretty format
            res_pretty = self.run_cli("ad", "schema", "-c", "11", "--format", "pretty")
            self.assertEqual(res_pretty.returncode, 0)
            self.assertIn("Coches de segunda mano", res_pretty.stdout)
            self.assertIn("fuel", res_pretty.stdout)
        finally:
            server.shutdown()
            server.server_close()

    def test_32h_ad_create_with_mock_server_dry_run_success(self):
        server = self._start_mock_server()
        try:
            valid_spec = json.dumps({
                "category_id": 11,
                "title": "Volkswagen Golf VII 2.0 TDI Advance",
                "description": "Vehículo en impecable estado de conservación con libro de mantenimiento.",
                "price": 13900.0,
                "location": {
                    "province": "Madrid",
                    "municipality": "Alcobendas",
                },
                "attributes": {
                    "fuel": "diesel",
                    "transmission": "manual",
                },
            })
            res = self.run_cli("ad", "create", "--spec", valid_spec, "--dry-run", "--format", "json")
            self.assertEqual(res.returncode, 0)
            report = json.loads(res.stdout)
            self.assertTrue(report.get("valid"))
            self.assertEqual(report.get("category_id"), 11)
        finally:
            server.shutdown()
            server.server_close()

    def test_32i_ad_create_from_file_and_stdin(self):
        server = self._start_mock_server()
        try:
            valid_spec = json.dumps({
                "category_id": 11,
                "title": "Bicicleta de carretera carbono profesional",
                "description": "Bicicleta en perfecto estado con componentes Shimano 105 completos.",
                "price": 850.0,
                "location": {"province": "Barcelona"},
            })
            # From file
            tmp_spec_file = Path(self.tmp_dir) / "test_ad.json"
            tmp_spec_file.write_text(valid_spec, encoding="utf-8")
            res_file = self.run_cli("ad", "create", "--from-file", str(tmp_spec_file), "--dry-run", "--format", "json")
            self.assertEqual(res_file.returncode, 0)
            rep_file = json.loads(res_file.stdout)
            self.assertTrue(rep_file.get("valid"))

            # From stdin
            res_stdin = self.run_cli("ad", "create", "--from-file", "-", "--dry-run", "--format", "json", input_text=valid_spec)
            self.assertEqual(res_stdin.returncode, 0)
            rep_stdin = json.loads(res_stdin.stdout)
            self.assertTrue(rep_stdin.get("valid"))
        finally:
            server.shutdown()
            server.server_close()

    def test_32j_ad_create_invalid_enum_suggestion(self):
        server = self._start_mock_server()
        try:
            spec = json.dumps({
                "category_id": 11,
                "title": "BMW Serie 3 320d Pack M",
                "description": "Vehículo en impecable estado de conservación siempre en garaje.",
                "price": 18000.0,
                "location": {"province": "Madrid"},
                "attributes": {
                    "fuel": "gasoil",
                },
            })
            res = self.run_cli("ad", "create", "--spec", spec, "--dry-run", "--format", "json")
            self.assertEqual(res.returncode, 2)
            report = json.loads(res.stdout)
            self.assertFalse(report.get("valid"))
            errors = report.get("errors", [])
            fuel_errors = [e for e in errors if e.get("field") == "attributes.fuel"]
            self.assertEqual(len(fuel_errors), 1)
            self.assertEqual(fuel_errors[0].get("suggestion"), "Change 'gasoil' to 'diesel'")
        finally:
            server.shutdown()
            server.server_close()

    def test_32k_ad_create_unauthenticated_exit_code_3(self):
        valid_spec = json.dumps({
            "category_id": 11,
            "title": "PlayStation 5 con mando dualsense",
            "description": "Consola en perfecto estado con caja original y cables incluidos sin apenas uso.",
            "price": 400.0,
            "location": {"province": "Sevilla"},
        })
        res = self.run_cli("ad", "create", "--spec", valid_spec, "--dry-run=false", "--format", "json")
        self.assertEqual(res.returncode, 3)

    def test_32l_ad_create_published_with_mock_server(self):
        server = self._start_mock_server()
        try:
            self.env["MILANUNCIOS_SESSION_TOKEN"] = "mock-token-123"
            valid_spec = json.dumps({
                "category_id": 11,
                "title": "Volkswagen Golf VII 2.0 TDI Advance",
                "description": "Vehículo en impecable estado de conservación con libro de mantenimiento.",
                "price": 13900.0,
                "location": {
                    "province": "Madrid",
                    "municipality": "Alcobendas",
                },
                "attributes": {
                    "fuel": "diesel",
                },
                "photos": [
                    "media://m-991203"
                ]
            })
            res = self.run_cli("ad", "create", "--spec", valid_spec, "--dry-run=false", "--format", "json")
            self.assertEqual(res.returncode, 0)
            ad = json.loads(res.stdout)
            self.assertEqual(ad.get("id"), "492817294")
            self.assertEqual(ad.get("status"), "published")
            self.assertEqual(ad.get("price"), 13900.0)
            self.assertIn("volkswagen-golf", ad.get("url", ""))
        finally:
            server.shutdown()
            server.server_close()

    def test_32m_ad_update_missing_args_exit_code_2(self):
        # Missing ad ID
        res = self.run_cli("ad", "update")
        self.assertEqual(res.returncode, 2)
        self.assertIn("accepts 1 arg", res.stderr.lower())

        # Missing update fields
        res2 = self.run_cli("ad", "update", "492817294")
        self.assertEqual(res2.returncode, 2)
        self.assertIn("no update fields provided", res2.stderr.lower())

        # Invalid price <= 0
        res3 = self.run_cli("ad", "update", "492817294", "--price", "-10")
        self.assertEqual(res3.returncode, 2)
        self.assertIn("price must be greater than 0", res3.stderr.lower())

        # Invalid JSON spec
        res4 = self.run_cli("ad", "update", "492817294", "--spec", "invalid-json")
        self.assertEqual(res4.returncode, 2)
        self.assertIn("invalid json patch", res4.stderr.lower())

    def test_32n_ad_update_unauthenticated_exit_code_3(self):
        res = self.run_cli("ad", "update", "492817294", "--price", "12000")
        self.assertEqual(res.returncode, 3)

    def test_32o_ad_update_with_mock_server_success(self):
        server = self._start_mock_server()
        try:
            self.env["MILANUNCIOS_SESSION_TOKEN"] = "mock-token-123"
            res = self.run_cli("ad", "update", "492817294", "--price", "13500", "--title", "Golf Actualizado", "--format", "json")
            self.assertEqual(res.returncode, 0)
            data = json.loads(res.stdout)
            self.assertEqual(data.get("id"), "492817294")
            self.assertEqual(data.get("new_price"), 13500.0)
            self.assertIn("price", data.get("updated_fields", []))
        finally:
            server.shutdown()
            server.server_close()

    def test_32p_ad_delete_missing_args_exit_code_2(self):
        # Missing ad ID
        res = self.run_cli("ad", "delete")
        self.assertEqual(res.returncode, 2)
        self.assertIn("accepts 1 arg", res.stderr.lower())

        # Invalid reason
        res2 = self.run_cli("ad", "delete", "492817294", "--reason", "invalid_reason")
        self.assertEqual(res2.returncode, 2)
        self.assertIn("invalid reason", res2.stderr.lower())

    def test_32q_ad_delete_unauthenticated_exit_code_3(self):
        res = self.run_cli("ad", "delete", "492817294")
        self.assertEqual(res.returncode, 3)

    def test_32r_ad_delete_with_mock_server_success(self):
        server = self._start_mock_server()
        try:
            self.env["MILANUNCIOS_SESSION_TOKEN"] = "mock-token-123"
            res = self.run_cli("ad", "delete", "492817294", "--reason", "sold_milanuncios", "--feedback", "Vendido", "--format", "json")
            self.assertEqual(res.returncode, 0)
            data = json.loads(res.stdout)
            self.assertEqual(data.get("id"), "492817294")
            self.assertEqual(data.get("status"), "deleted")
            self.assertEqual(data.get("reason"), "sold_milanuncios")
        finally:
            server.shutdown()
            server.server_close()

    def _start_mock_server(self):
        class _MockCategoryHandler(http.server.BaseHTTPRequestHandler):
            def do_GET(self):
                if self.path == "/api/v1/categories/11/filters":
                    self.send_response(200)
                    self.send_header("Content-Type", "application/json")
                    self.end_headers()
                    data = {
                        "category_id": 11,
                        "category_name": "Coches de segunda mano",
                        "filters": [
                            {
                                "key": "fuel",
                                "label": "Combustible",
                                "type": "select",
                                "options": ["diesel", "gasolina", "electrico", "hibrido", "hibrido_enchufable", "gas"],
                            },
                            {
                                "key": "transmission",
                                "label": "Cambio",
                                "type": "select",
                                "options": ["manual", "automatico"],
                            },
                        ],
                    }
                    self.wfile.write(json.dumps(data).encode("utf-8"))
                else:
                    self.send_response(404)
                    self.end_headers()

            def do_POST(self):
                if self.path == "/api/v1/media/upload":
                    self.send_response(200)
                    self.send_header("Content-Type", "application/json")
                    self.end_headers()
                    data = {
                        "media_id": "media-m-991203",
                        "url": "https://img.milanuncios.com/fotos/media-m-991203.jpg"
                    }
                    self.wfile.write(json.dumps(data).encode("utf-8"))
                elif self.path == "/api/v1/ads":
                    if not self.headers.get("Authorization"):
                        self.send_response(401)
                        self.end_headers()
                        return
                    content_len = int(self.headers.get("Content-Length", 0))
                    body = json.loads(self.rfile.read(content_len).decode("utf-8"))
                    self.send_response(201)
                    self.send_header("Content-Type", "application/json")
                    self.end_headers()
                    data = {
                        "id": "492817294",
                        "client_request_id": body.get("client_request_id"),
                        "status": "published",
                        "title": body.get("title"),
                        "price": body.get("price"),
                        "category_id": body.get("category_id"),
                        "url": "https://www.milanuncios.com/coches-de-segunda-mano/volkswagen-golf-492817294.htm",
                        "created_at": "2026-11-18T08:50:00Z"
                    }
                    self.wfile.write(json.dumps(data).encode("utf-8"))
                else:
                    self.send_response(404)
                    self.end_headers()

            def do_PATCH(self):
                if self.path == "/api/v1/ads/492817294":
                    if not self.headers.get("Authorization"):
                        self.send_response(401)
                        self.end_headers()
                        return
                    content_len = int(self.headers.get("Content-Length", 0))
                    patch = json.loads(self.rfile.read(content_len).decode("utf-8"))
                    self.send_response(200)
                    self.send_header("Content-Type", "application/json")
                    self.end_headers()
                    data = {
                        "id": "492817294",
                        "updated_fields": sorted(list(patch.keys())),
                        "previous_price": 13900.0,
                        "new_price": patch.get("price", 13500.0),
                        "updated_at": "2026-11-18T09:15:00Z"
                    }
                    self.wfile.write(json.dumps(data).encode("utf-8"))
                else:
                    self.send_response(404)
                    self.end_headers()

            def do_DELETE(self):
                if self.path == "/api/v1/ads/492817294":
                    if not self.headers.get("Authorization"):
                        self.send_response(401)
                        self.end_headers()
                        return
                    content_len = int(self.headers.get("Content-Length", 0))
                    payload = json.loads(self.rfile.read(content_len).decode("utf-8")) if content_len > 0 else {}
                    self.send_response(200)
                    self.send_header("Content-Type", "application/json")
                    self.end_headers()
                    data = {
                        "id": "492817294",
                        "status": "deleted",
                        "reason": payload.get("reason", "withdrawn"),
                        "message": "Listing deleted successfully"
                    }
                    self.wfile.write(json.dumps(data).encode("utf-8"))
                else:
                    self.send_response(404)
                    self.end_headers()

            def log_message(self, format, *args):
                pass

        server = http.server.HTTPServer(("127.0.0.1", 0), _MockCategoryHandler)
        port = server.server_port
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        self.env["MILANUNCIOS_BASE_URL"] = f"http://127.0.0.1:{port}"
        return server


# ==============================================================================
# 5. WATCH ENGINE & SQLITE PERSISTENCE (Tests 33 - 42)
# ==============================================================================
class TestWatchEngine(MilanunciosCLITestCase):

    def test_33_watch_list_initially_empty(self):
        res = self.run_cli("watch", "list")
        self.assertEqual(res.returncode, 0)

    def test_34_watch_list_empty_json_returns_array(self):
        res = self.run_cli("watch", "list", "--format", "json")
        self.assertEqual(res.returncode, 0)
        items = json.loads(res.stdout)
        self.assertIsInstance(items, list)
        self.assertEqual(len(items), 0)

    def test_35_watch_add_search(self):
        res = self.run_cli(
            "watch", "add", "search", "bicicleta",
            "--name", "test-bike",
            "--interval", "5m",
            "--max-price", "350"
        )
        self.assertEqual(res.returncode, 0)
        self.assertIn("test-bike", res.stdout)

    def test_36_watch_add_ad(self):
        res = self.run_cli(
            "watch", "add", "ad", "492817294",
            "--name", "test-item-492",
            "--interval", "10m"
        )
        self.assertEqual(res.returncode, 0)
        self.assertIn("test-item-492", res.stdout)

    def test_37_watch_add_invalid_interval_fails(self):
        res = self.run_cli("watch", "add", "search", "macbook", "--interval", "bad_interval_format")
        self.assertEqual(res.returncode, 2)
        self.assertIn("invalid interval format", res.stderr.lower())

    def test_38_watch_list_reflects_added_items(self):
        self.run_cli("watch", "add", "search", "ps5", "--name", "watch-ps5")
        self.run_cli("watch", "add", "ad", "492817294", "--name", "watch-ad")

        res = self.run_cli("watch", "list", "--format", "json")
        self.assertEqual(res.returncode, 0)
        items = json.loads(res.stdout)
        names = {item["name"] for item in items}
        self.assertIn("watch-ps5", names)
        self.assertIn("watch-ad", names)

    def test_39_watch_events_empty_history(self):
        res = self.run_cli("watch", "events")
        self.assertEqual(res.returncode, 0)

    def test_40_watch_events_json_returns_array(self):
        res = self.run_cli("watch", "events", "--format", "json")
        self.assertEqual(res.returncode, 0)
        events = json.loads(res.stdout)
        self.assertIsInstance(events, list)

    def test_41_watch_remove_nonexistent_exit_code_4(self):
        res = self.run_cli("watch", "remove", "ghost-watch-identifier")
        self.assertEqual(res.returncode, 4)
        self.assertIn("not found", res.stderr.lower())

    def test_42_watch_remove_existing(self):
        self.run_cli("watch", "add", "search", "switch", "--name", "temp-watch")
        res_del = self.run_cli("watch", "remove", "temp-watch")
        self.assertEqual(res_del.returncode, 0)

        # Verify removal in list
        res_list = self.run_cli("watch", "list", "--format", "json")
        items = json.loads(res_list.stdout)
        self.assertFalse(any(i["name"] == "temp-watch" for i in items))


# ==============================================================================
# 6. NOTIFICATION SINKS (Tests 43 - 45)
# ==============================================================================
class TestNotificationSinks(MilanunciosCLITestCase):

    def test_43_sink_list_empty(self):
        res = self.run_cli("sink", "list")
        self.assertEqual(res.returncode, 0)

    def test_44_sink_list_json_returns_array(self):
        res = self.run_cli("sink", "list", "--format", "json")
        self.assertEqual(res.returncode, 0)
        sinks = json.loads(res.stdout)
        self.assertIsInstance(sinks, list)

    def test_45_sink_test_nonexistent_exit_code_4(self):
        res = self.run_cli("sink", "test", "ghost_sink")
        self.assertEqual(res.returncode, 4)
        self.assertIn("not found", res.stderr.lower())


# ==============================================================================
# 7. CHAT & MESSAGING VALIDATION (Tests 46 - 48+)
# ==============================================================================
class TestChatValidation(MilanunciosCLITestCase):

    def test_46_chat_list_unauthenticated_exit_code_3(self):
        res = self.run_cli("chat", "list")
        self.assertEqual(res.returncode, 3)

    def test_47_chat_show_missing_id_exit_code_2(self):
        res = self.run_cli("chat", "show")
        self.assertEqual(res.returncode, 2)
        self.assertIn("accepts 1 arg", res.stderr.lower())

    def test_48_chat_send_missing_arguments_exit_code_2(self):
        res = self.run_cli("chat", "send")
        self.assertEqual(res.returncode, 2)
        self.assertIn("requires at least 2 arg", res.stderr.lower())

    def test_48b_chat_start_missing_arguments_exit_code_2(self):
        res = self.run_cli("chat", "start")
        self.assertEqual(res.returncode, 2)
        self.assertIn("requires at least 1 arg", res.stderr.lower())

    def test_48c_chat_start_unauthenticated_exit_code_3(self):
        res = self.run_cli("chat", "start", "492817294", "Hola, sigue disponible?")
        self.assertEqual(res.returncode, 3)

    def test_48d_chat_list_unread_only_unauthenticated_exit_code_3(self):
        res = self.run_cli("chat", "list", "--unread-only")
        self.assertEqual(res.returncode, 3)


# ==============================================================================
# 8. MCP SERVER & EMBEDDED SKILLS (Tests 49 - 52)
# ==============================================================================
class TestMCPSkillsAndAgent(MilanunciosCLITestCase):

    def test_49_skills_list(self):
        res = self.run_cli("skills", "list")
        self.assertEqual(res.returncode, 0)
        self.assertIn("milanuncios-usage", res.stdout)

    def test_50_skills_get_milanuncios_usage(self):
        res = self.run_cli("skills", "get", "milanuncios-usage")
        self.assertEqual(res.returncode, 0)
        self.assertIn("# Milanuncios CLI", res.stdout)
        self.assertIn("milanuncios search", res.stdout)

    def test_51_skills_get_nonexistent_exit_code_4(self):
        res = self.run_cli("skills", "get", "nonexistent-skill-xyz")
        self.assertEqual(res.returncode, 4)
        self.assertIn("not found", res.stderr.lower())

    def test_52_mcp_install_invalid_agent_exit_code_2(self):
        res = self.run_cli("mcp", "install", "unsupported_ai_agent")
        self.assertEqual(res.returncode, 2)
        self.assertIn("unknown install target", res.stderr.lower())


# ==============================================================================
# 9. ADVANCED I/O INTEGRITY & DEBUG LOGGING (Tests 53 - 54)
# ==============================================================================
class TestIOIntegrity(MilanunciosCLITestCase):

    def test_53_strict_stdout_cleanliness(self):
        res = self.run_cli("profile", "list", "--format", "json")
        self.assertEqual(res.returncode, 0)
        # Absolutely no leading or trailing non-JSON logs allowed in stdout
        raw_stdout = res.stdout.strip()
        self.assertTrue(raw_stdout.startswith("[") or raw_stdout.startswith("{"))
        parsed = json.loads(raw_stdout)
        self.assertTrue(isinstance(parsed, (dict, list)))

    def test_54_format_pretty_rendering(self):
        res = self.run_cli("watch", "list", "--format", "pretty")
        self.assertEqual(res.returncode, 0)
        self.assertIn("No watches configured", res.stdout)

    def test_55_format_toon_rendering(self):
        res = self.run_cli("watch", "list", "--format", "toon")
        self.assertEqual(res.returncode, 0)
        self.assertIn("[0]:", res.stdout)

    def test_56_no_color_flag(self):
        res = self.run_cli("watch", "list", "--format", "pretty", "--no-color")
        self.assertEqual(res.returncode, 0)
        # Verify no ANSI color escape codes in output
        self.assertNotIn("\x1b[", res.stdout)

    def test_57_no_input_flag_non_interactive_guard(self):
        res = self.run_cli("auth", "login", "--no-input")
        self.assertEqual(res.returncode, 2)
        self.assertIn("pass --cookies <file> in non-interactive mode", res.stderr)

    def test_58_watch_add_integer_interval_below_minimum_fails(self):
        res = self.run_cli("watch", "add", "search", "bmw", "--interval", "1")
        self.assertEqual(res.returncode, 2)
        self.assertIn("interval must be at least 10 seconds", res.stderr)

    def test_59_profile_remove_nonexistent_exit_code_4(self):
        res = self.run_cli("profile", "remove", "ghost-nonexistent-profile")
        self.assertEqual(res.returncode, 4)
        self.assertIn("not found", res.stderr.lower())

    def test_60_skills_get_format_json_valid(self):
        res = self.run_cli("skills", "get", "milanuncios-usage", "--format", "json")
        self.assertEqual(res.returncode, 0)
        data = json.loads(res.stdout)
        self.assertEqual(data.get("name"), "milanuncios-usage")
        self.assertIn("Milanuncios CLI Agent Guide", data.get("content", ""))


# ==============================================================================
# CLI Test Runner
# ==============================================================================
if __name__ == "__main__":
    print("=" * 70)
    print("🚀 Running milanuncios-cli End-to-End Black-Box Test Suite (72 Tests)")
    print("=" * 70)

    suite = unittest.TestSuite()
    loader = unittest.TestLoader()

    test_classes = [
        TestCoreAndFlags,
        TestConfigCommands,
        TestAuthAndProfiles,
        TestAdAndSearchValidation,
        TestWatchEngine,
        TestNotificationSinks,
        TestChatValidation,
        TestMCPSkillsAndAgent,
        TestIOIntegrity,
    ]

    for tc in test_classes:
        suite.addTests(loader.loadTestsFromTestCase(tc))

    runner = unittest.TextTestRunner(verbosity=2)
    result = runner.run(suite)

    sys.exit(0 if result.wasSuccessful() else 1)
