import assert from "node:assert/strict";
import { isValidElement, type ReactElement } from "react";
import { Cursor } from "./index.ts";

type ElementProps = Record<string, unknown>;
type ElementWithProps = ReactElement<ElementProps>;

function asElement(value: unknown): ElementWithProps {
  assert.equal(isValidElement(value), true);
  return value as ElementWithProps;
}

function childrenOf(element: ElementWithProps): unknown[] {
  return Array.isArray(element.props.children) ? element.props.children : [element.props.children];
}

const labeledCursor = asElement(
  Cursor({
    cursor: { x: 24, y: 36, mode: "comment", label: "fallback-label" },
    user: { id: "user-1", name: "Ada" },
    color: "#ffffff",
    coordinateSpace: "percent",
  }),
);
assert.equal(labeledCursor.props["data-openrtc-cursor"], "comment");
assert.deepEqual(
  {
    left: (labeledCursor.props.style as ElementProps)["left"],
    top: (labeledCursor.props.style as ElementProps)["top"],
    color: (labeledCursor.props.style as ElementProps)["color"],
  },
  { left: "24%", top: "36%", color: "#ffffff" },
);

const labeledChildren = childrenOf(labeledCursor);
assert.equal(labeledChildren.length, 2);
const label = asElement(labeledChildren[1]);
assert.equal(label.props.children, "Ada");
assert.equal((label.props.style as ElementProps)["color"], "#111827");

const pixelCursor = asElement(
  Cursor({
    cursor: { x: 120, y: 80 },
    coordinateSpace: "pixel",
    showLabel: false,
  }),
);
assert.equal(pixelCursor.props["data-openrtc-cursor"], "pointer");
assert.deepEqual(
  {
    left: (pixelCursor.props.style as ElementProps)["left"],
    top: (pixelCursor.props.style as ElementProps)["top"],
  },
  { left: 120, top: 80 },
);
assert.equal(childrenOf(pixelCursor)[1], null);

const clampedCursor = asElement(
  Cursor({
    cursor: { x: -40, y: 140 },
    coordinateSpace: "percent",
  }),
);
assert.deepEqual(
  {
    left: (clampedCursor.props.style as ElementProps)["left"],
    top: (clampedCursor.props.style as ElementProps)["top"],
  },
  { left: "0%", top: "100%" },
);
