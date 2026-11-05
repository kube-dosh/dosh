import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { StatusBadge } from "./status-badge";

describe("StatusBadge", () => {
  it("uses the accessible destructive text token for failure states", () => {
    render(<StatusBadge status="Failed" />);

    const badge = screen.getByText("Failed");
    expect(badge).toHaveClass("text-destructive-strong");
    expect(badge).not.toHaveClass("text-destructive");
  });
});
