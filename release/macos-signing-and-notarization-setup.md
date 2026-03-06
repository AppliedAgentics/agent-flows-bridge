# macOS Signing And Notarization Setup

**Created:** 2026-03-05

This runbook sets up the Apple credentials required for signed, notarized Homebrew releases of Agent Flows Bridge.

The GitHub Actions release workflow is configured to use:

- `Developer ID Application` certificate for code signing
- `App Store Connect API key` for notarization

## What You Need

- Apple Developer Program membership
- Access to the `AppliedAgentics/agent-flows-bridge` GitHub repository
- Access to the `AppliedAgentics/homebrew-tap` GitHub repository
- `gh` authenticated locally
- A macOS machine with Keychain Access

## Required GitHub Actions Secrets

The release workflow expects these secrets in `AppliedAgentics/agent-flows-bridge`:

- `APPLE_CERTIFICATE`
- `APPLE_CERTIFICATE_PASSWORD`
- `APPLE_SIGNING_IDENTITY`
- `APPLE_API_ISSUER`
- `APPLE_API_KEY`
- `APPLE_API_KEY_P8_BASE64`
- `HOMEBREW_TAP_PUSH_TOKEN`

## Part 1: Create The Developer ID Application Certificate

Open the required Apple pages and tools:

```bash
open "https://developer.apple.com/account/resources/certificates/list"
open -a "Keychain Access"
```

In Keychain Access:

1. Open `Keychain Access` -> `Certificate Assistant` -> `Request a Certificate From a Certificate Authority...`
2. Save the CSR file to disk

In the Apple Developer portal:

1. Create a new certificate
2. Choose `Developer ID Application`
3. Upload the CSR
4. Download the generated certificate

Install the downloaded certificate by opening it:

```bash
open "$HOME/Downloads"
```

After the certificate is installed, confirm the signing identity:

```bash
security find-identity -v -p codesigning
```

Copy the exact `Developer ID Application: ...` identity string.

Export the certificate and private key as a `.p12` file from Keychain Access:

1. Open `Keychain Access`
2. Find the `Developer ID Application` certificate
3. Right-click it
4. Choose `Export`
5. Save it as `agent-flows-bridge-developer-id.p12`
6. Set a strong export password

Base64-encode the exported `.p12`:

```bash
export P12_PATH="$HOME/Desktop/agent-flows-bridge-developer-id.p12"
base64 < "$P12_PATH" | tr -d '\n' > /tmp/agent-flows-bridge-developer-id.p12.b64
```

Store the signing secrets in GitHub:

```bash
gh secret set APPLE_CERTIFICATE --repo AppliedAgentics/agent-flows-bridge < /tmp/agent-flows-bridge-developer-id.p12.b64
gh secret set APPLE_CERTIFICATE_PASSWORD --repo AppliedAgentics/agent-flows-bridge
gh secret set APPLE_SIGNING_IDENTITY --repo AppliedAgentics/agent-flows-bridge
```

When prompted:

- `APPLE_CERTIFICATE_PASSWORD` should be the `.p12` export password
- `APPLE_SIGNING_IDENTITY` should be the exact `Developer ID Application: ...` value from `security find-identity`

## Part 2: Create The App Store Connect API Key

Open App Store Connect:

```bash
open "https://appstoreconnect.apple.com/access/integrations/api"
```

In App Store Connect:

1. Go to `Users and Access`
2. Open the `Integrations` tab
3. Create a new API key with access suitable for notarization
4. Download the `.p8` key file
5. Copy the `Key ID`
6. Copy the `Issuer ID`

Base64-encode the downloaded `.p8`:

```bash
export APPLE_API_KEY_FILE="$HOME/Downloads/AuthKey_XXXXXXXXXX.p8"
base64 < "$APPLE_API_KEY_FILE" | tr -d '\n' > /tmp/agent-flows-bridge-authkey.p8.b64
```

Store the notarization secrets in GitHub:

```bash
gh secret set APPLE_API_KEY --repo AppliedAgentics/agent-flows-bridge
gh secret set APPLE_API_ISSUER --repo AppliedAgentics/agent-flows-bridge
gh secret set APPLE_API_KEY_P8_BASE64 --repo AppliedAgentics/agent-flows-bridge < /tmp/agent-flows-bridge-authkey.p8.b64
```

When prompted:

- `APPLE_API_KEY` should be the App Store Connect `Key ID`
- `APPLE_API_ISSUER` should be the App Store Connect `Issuer ID`

## Part 3: Verify The Secrets Exist

Run:

```bash
gh secret list --repo AppliedAgentics/agent-flows-bridge
```

You should see:

- `APPLE_CERTIFICATE`
- `APPLE_CERTIFICATE_PASSWORD`
- `APPLE_SIGNING_IDENTITY`
- `APPLE_API_ISSUER`
- `APPLE_API_KEY`
- `APPLE_API_KEY_P8_BASE64`
- `HOMEBREW_TAP_PUSH_TOKEN`

## Part 4: Cut A Signed Release

Prepare the next release metadata:

```bash
cd /Users/sidneyl/code/agent-flows-bridge
python3 scripts/release_macos.py \
  --prepare-release \
  --change "Summarize the first user-visible change" \
  --change "Summarize the second user-visible change"
```

Review and commit:

```bash
cd /Users/sidneyl/code/agent-flows-bridge
git status --short
git diff
git add .
git commit -m "Prepare release"
git push origin main
```

Create and push the release tag:

```bash
cd /Users/sidneyl/code/agent-flows-bridge
git tag vYYYY.MM.DD.XX
git push origin vYYYY.MM.DD.XX
```

Watch the release workflow:

```bash
gh run list --repo AppliedAgentics/agent-flows-bridge --workflow "Agent Flows Bridge Release" --limit 5
gh run watch --repo AppliedAgentics/agent-flows-bridge
```

## Part 5: Verify The Distributed App

After the workflow completes, verify the release and install path:

```bash
gh release view vYYYY.MM.DD.XX --repo AppliedAgentics/agent-flows-bridge
brew tap AppliedAgentics/tap
brew install --cask agent-flows-bridge
open -a "Agent Flows Bridge"
```

If you need a clean reinstall:

```bash
brew uninstall --cask agent-flows-bridge || true
rm -rf "$HOME/Applications/Agent Flows Bridge.app"
rm -rf "/Applications/Agent Flows Bridge.app"
brew tap AppliedAgentics/tap
brew install --cask agent-flows-bridge
open -a "Agent Flows Bridge"
```
