# typed: strict
# frozen_string_literal: true

cask "agent-flows-bridge" do
  version "0.1.0"
  sha256 "54b9a4d6c104cd60c17a1d73be92b19b7be86b11a455402537ad34cfb81c52f3"

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
