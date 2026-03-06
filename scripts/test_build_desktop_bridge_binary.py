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

    def test_signing_commands_enable_timestamp_and_hardened_runtime(self):
        config = build_desktop_bridge_binary.ReleaseSigningConfig(
            certificate_base64="encoded-certificate",
            certificate_password="certificate-password",
            signing_identity="Developer ID Application: Applied Agentics, Inc. (TEAMID1234)",
        )

        commands = build_desktop_bridge_binary.signing_commands(
            binary_path=pathlib.Path("/tmp/agent-flows-bridge"),
            certificate_path=pathlib.Path("/tmp/developer-id.p12"),
            keychain_path=pathlib.Path("/tmp/bridge-signing.keychain-db"),
            keychain_password="temporary-keychain-password",
            config=config,
        )

        rendered = [" ".join(command) for command in commands]

        self.assertIn(
            "security import /tmp/developer-id.p12 -k /tmp/bridge-signing.keychain-db -P certificate-password -T /usr/bin/codesign -T /usr/bin/security",
            rendered,
        )
        self.assertIn(
            "security set-key-partition-list -S apple-tool:,apple: -s -k temporary-keychain-password /tmp/bridge-signing.keychain-db",
            rendered,
        )
        self.assertIn(
            "codesign --force --sign Developer ID Application: Applied Agentics, Inc. (TEAMID1234) --keychain /tmp/bridge-signing.keychain-db --timestamp --options runtime --verbose=4 /tmp/agent-flows-bridge",
            rendered,
        )
