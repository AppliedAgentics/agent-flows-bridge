# typed: strict
# frozen_string_literal: true

cask "agent-flows-bridge" do
  version "2026.03.05.04"
  sha256 "bf8db34103ba1ab57f911dfa09246d5e5963d2555c4b1c0a8304f66e043e9fc6"

  url "https://github.com/AppliedAgentics/agent-flows-bridge/releases/download/v#{version}/agent-flows-bridge-#{version}-macos.zip"
  name "Agent Flows Bridge"
  desc "Desktop bridge for connecting local OpenClaw runtimes to Agent Flows"
  homepage "https://github.com/AppliedAgentics/agent-flows-bridge"

  app "Agent Flows Bridge.app"

  zap trash: [
    "~/.agent-flows-bridge",
    "~/Library/Application Support/ai.appliedagentics.agentflowsbridge",
    "~/Library/Caches/ai.appliedagentics.agentflowsbridge",
    "~/Library/Preferences/ai.appliedagentics.agentflowsbridge.plist",
    "~/Library/Saved Application State/ai.appliedagentics.agentflowsbridge.savedState",
  ]
end
