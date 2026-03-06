# typed: strict
# frozen_string_literal: true

cask "agent-flows-bridge" do
  version "2026.03.05.03"
  sha256 "9fcff7141428c7f000994057e91afca3c9466f49d2ee34a737264fa1f0d503d3"

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
