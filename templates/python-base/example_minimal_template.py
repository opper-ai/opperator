#!/usr/bin/env python3

from opperator import OpperatorAgent, LogLevel


class NewAgent(OpperatorAgent):
    """A minimal agent with just the essentials."""

    def initialize(self) -> None:
        self.set_description("A minimal example agent")
        self.log(LogLevel.INFO, "Agent initialized")

    def start(self) -> None:
        self.log(LogLevel.INFO, "Agent started")


if __name__ == "__main__":
    NewAgent().run()
