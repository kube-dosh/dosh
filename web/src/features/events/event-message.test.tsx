import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { EventMessage } from "@/features/events/event-message";

describe("EventMessage", () => {
  it("clamps the list message and exposes the full content in a dialog", async () => {
    const message = `Warning: ${"container failed to pull image ".repeat(20)}`;
    render(<EventMessage message={message} />);

    const trigger = screen.getByRole("button", { name: /Warning: container failed to pull image/ });
    expect(trigger).toHaveAttribute("aria-haspopup", "dialog");
    expect(trigger.querySelector("span")).toHaveClass("line-clamp-2");

    fireEvent.click(trigger);

    const dialog = await screen.findByRole("dialog", { name: "事件消息" });
    expect(within(dialog).getByText(/container failed to pull image/)).toBeInTheDocument();
  });
});
