# typed: strict
# frozen_string_literal: true

cask "agent-flows-bridge" do
  version "2026.03.05.07"
  sha256 "6cb8984d228434ea72459469733ee6be51d67777a3e1a617667655da2efd8212"

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
