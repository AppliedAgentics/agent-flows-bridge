# typed: strict
# frozen_string_literal: true

cask "agent-flows-bridge" do
  version "0.1.1"
  sha256 "51d620d4c9ab32b469e594808655eec2fdc592f8ee1c09f152fa271b7f0aa24f"

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
