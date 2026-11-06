import { createColumnHelper } from "@tanstack/react-table";
import type { EventItem } from "@/api/types";
import { EventMessage } from "@/features/events/event-message";

const column = createColumnHelper<EventItem>();

export const eventMessageColumn = column.accessor("message", {
  header: "消息",
  size: 420,
  meta: {
    width: "48%",
    minWidth: 420,
    maxWidth: 760,
    headerClassName: "whitespace-normal",
    cellClassName: "whitespace-normal align-top",
  },
  cell: (info) => <EventMessage message={info.getValue()} />,
});
