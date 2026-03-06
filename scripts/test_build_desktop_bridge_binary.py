import pathlib
import unittest

from scripts import build_desktop_bridge_binary


class BuildDesktopBridgeBinaryTests(unittest.TestCase):
    def test_release_signing_config_detects_valid_developer_id_environment(self):
        config = build_desktop_bridge_binary.release_signing_config(
            {
                "APPLE_CERTIFICATE": "encoded-certificate",
                "APPLE_CERTIFICATE_PASSWORD": "certificate-password",
                "APPLE_SIGNING_IDENTITY": "Developer ID Application: Applied Agentics, Inc. (TEAMID1234)",
            },
            system_name="Darwin",
        )

        self.assertIsNotNone(config)
        self.assertEqual(config.certificate_base64, "encoded-certificate")
        self.assertEqual(config.certificate_password, "certificate-password")
        self.assertEqual(
            config.signing_identity,
            "Developer ID Application: Applied Agentics, Inc. (TEAMID1234)",
        )

    def test_release_signing_config_ignores_non_developer_id_identities(self):
        config = build_desktop_bridge_binary.release_signing_config(
            {
                "APPLE_CERTIFICATE": "encoded-certificate",
                "APPLE_CERTIFICATE_PASSWORD": "certificate-password",
                "APPLE_SIGNING_IDENTITY": "-",
            },
            system_name="Darwin",
        )

        self.assertIsNone(config)

    def test_parse_codesign_identity_reference_prefers_matching_identity_label(self):
        identity_reference = build_desktop_bridge_binary.parse_codesign_identity_reference(
            '\n'.join(
                [
                    '  1) AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA "Developer ID Application: Wrong Team (TEAMID0000)"',
                    '  2) E658F359E336D09F147678CFDBF02815B6676D46 "Developer ID Application: Applied Agentics, Inc. (TEAMID1234)"',
                    '     2 valid identities found',
                ]
            ),
            "Developer ID Application: Applied Agentics, Inc. (TEAMID1234)",
        )

        self.assertEqual(identity_reference, "E658F359E336D09F147678CFDBF02815B6676D46")

    def test_parse_codesign_identity_reference_falls_back_to_first_identity(self):
        identity_reference = build_desktop_bridge_binary.parse_codesign_identity_reference(
            '\n'.join(
                [
                    '  1) AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA "***"',
                    '     1 valid identities found',
                ]
            ),
            "Developer ID Application: Applied Agentics, Inc. (TEAMID1234)",
        )

        self.assertEqual(identity_reference, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")

    def test_codesign_command_enables_timestamp_and_hardened_runtime(self):
        command = build_desktop_bridge_binary.codesign_command(
            binary_path=pathlib.Path("/tmp/agent-flows-bridge"),
            keychain_path=pathlib.Path("/tmp/bridge-signing.keychain-db"),
            signing_identity_reference="E658F359E336D09F147678CFDBF02815B6676D46",
        )

        self.assertIn(
            "--timestamp",
            command,
        )
        self.assertIn(
            "--options",
            command,
        )
        self.assertIn(
            "runtime",
            command,
        )
        self.assertEqual(
            command,
            [
                "codesign",
                "--force",
                "--sign",
                "E658F359E336D09F147678CFDBF02815B6676D46",
                "--keychain",
                "/tmp/bridge-signing.keychain-db",
                "--timestamp",
                "--options",
                "runtime",
                "--verbose=4",
                "/tmp/agent-flows-bridge",
            ],
        )
