"use client";

import { Checkbox, Table } from "@heroui/react";
import { useMemo, useState, type CSSProperties, type ReactNode } from "react";
import type { Selection, SortDescriptor, SortDirection } from "react-aria-components/Table";
import type { DragAndDropHooks } from "react-aria-components/useDragAndDrop";

export type ColumnSize = number | `${number}` | `${number}%` | `${number}fr`;

export interface DataGridColumn<T> {
  id: string;
  header: ReactNode | ((info: { sortDirection?: SortDirection }) => ReactNode);
  accessorKey?: keyof T & string;
  cell?: (item: T, column: DataGridColumn<T>) => ReactNode;
  isRowHeader?: boolean;
  allowsSorting?: boolean;
  sortFn?: (a: T, b: T) => number;
  allowsResizing?: boolean;
  width?: ColumnSize;
  minWidth?: number;
  maxWidth?: number;
  align?: "start" | "center" | "end";
  headerClassName?: string;
  cellClassName?: string;
  pinned?: "start" | "end";
}

export interface DataGridReorderEvent<T> {
  keys: Set<string | number>;
  target: {
    key: string | number;
    dropPosition: "before" | "after";
  };
  reorderedData: T[];
}

export interface DataGridProps<T extends object> {
  data: T[];
  columns: DataGridColumn<T>[];
  getRowId: (item: T) => string | number;
  variant?: "primary" | "secondary";
  "aria-label": string;
  className?: string;
  contentClassName?: string;
  scrollContainerClassName?: string;
  verticalAlign?: "top" | "middle" | "bottom";
  selectionMode?: "none" | "single" | "multiple";
  selectedKeys?: Selection;
  defaultSelectedKeys?: Selection;
  onSelectionChange?: (keys: Selection) => void;
  selectionBehavior?: "toggle" | "replace";
  showSelectionCheckboxes?: boolean;
  sortDescriptor?: SortDescriptor;
  defaultSortDescriptor?: SortDescriptor;
  onSortChange?: (descriptor: SortDescriptor) => void;
  allowsColumnResize?: boolean;
  onColumnResize?: (widths: Map<string | number, ColumnSize>) => void;
  onColumnResizeEnd?: (widths: Map<string | number, ColumnSize>) => void;
  onReorder?: (event: DataGridReorderEvent<T>) => void;
  dragAndDropHooks?: DragAndDropHooks;
  onRowAction?: (key: string | number) => void;
  renderEmptyState?: () => ReactNode;
  onLoadMore?: () => void;
  isLoadingMore?: boolean;
  loadMoreContent?: ReactNode;
  disabledKeys?: Iterable<string | number>;
  virtualized?: boolean;
  rowHeight?: number;
  headingHeight?: number;
  getChildren?: (item: T) => T[] | undefined;
  treeColumn?: string;
  expandedKeys?: Selection;
  defaultExpandedKeys?: Selection;
  onExpandedChange?: (keys: Selection) => void;
  treeIndent?: number;
}

function mergeClassNames(...classNames: Array<string | undefined>) {
  return classNames.filter(Boolean).join(" ");
}

function toCssSize(value: ColumnSize | undefined) {
  if (typeof value === "number") return value;
  return value;
}

function getPinnedOffset<T>(columns: DataGridColumn<T>[], index: number) {
  const column = columns[index];
  if (!column?.pinned) return undefined;
  const candidates = column.pinned === "start" ? columns.slice(0, index) : columns.slice(index + 1);
  const pinned = candidates.filter((candidate) => candidate.pinned === column.pinned);
  if (!pinned.every((candidate) => typeof candidate.width === "number")) return 0;
  return pinned.reduce((sum, candidate) => sum + Number(candidate.width), 0);
}

function getPinnedStyle<T>(columns: DataGridColumn<T>[], index: number): CSSProperties | undefined {
  const column = columns[index];
  if (!column?.pinned) return undefined;
  const offset = getPinnedOffset(columns, index);
  return column.pinned === "start" ? { insetInlineStart: offset } : { insetInlineEnd: offset };
}

function isPinnedEdge<T>(columns: DataGridColumn<T>[], index: number) {
  const column = columns[index];
  if (!column?.pinned) return false;
  if (column.pinned === "start") {
    return !columns.slice(index + 1).some((candidate) => candidate.pinned === "start");
  }
  return !columns.slice(0, index).some((candidate) => candidate.pinned === "end");
}

function defaultCompare(left: unknown, right: unknown) {
  return new Intl.Collator(undefined, { numeric: true, sensitivity: "base" }).compare(
    String(left ?? ""),
    String(right ?? ""),
  );
}

function sortRows<T extends object>(
  data: T[],
  columns: DataGridColumn<T>[],
  descriptor?: SortDescriptor,
) {
  if (!descriptor?.column) return data;
  const column = columns.find((candidate) => candidate.id === String(descriptor.column));
  if (!column) return data;
  const direction = descriptor.direction === "descending" ? -1 : 1;
  const compare = column.sortFn
    ? column.sortFn
    : (left: T, right: T) =>
        column.accessorKey
          ? defaultCompare(left[column.accessorKey], right[column.accessorKey])
          : 0;
  return [...data].sort((left, right) => compare(left, right) * direction);
}

export function DataGrid<T extends object>({
  "aria-label": ariaLabel,
  allowsColumnResize: _allowsColumnResize,
  className,
  columns,
  contentClassName,
  data,
  defaultExpandedKeys: _defaultExpandedKeys,
  defaultSelectedKeys,
  defaultSortDescriptor,
  disabledKeys,
  dragAndDropHooks,
  expandedKeys: _expandedKeys,
  getChildren: _getChildren,
  getRowId,
  headingHeight = 36,
  isLoadingMore: _isLoadingMore,
  loadMoreContent: _loadMoreContent,
  onColumnResize: _onColumnResize,
  onColumnResizeEnd: _onColumnResizeEnd,
  onExpandedChange: _onExpandedChange,
  onLoadMore: _onLoadMore,
  onReorder: _onReorder,
  onRowAction,
  onSelectionChange,
  onSortChange,
  renderEmptyState,
  rowHeight = 42,
  scrollContainerClassName,
  selectedKeys,
  selectionBehavior = "toggle",
  selectionMode = "none",
  showSelectionCheckboxes = false,
  sortDescriptor,
  treeColumn: _treeColumn,
  treeIndent: _treeIndent = 20,
  variant = "primary",
  verticalAlign = "middle",
  virtualized = false,
}: DataGridProps<T>) {
  const [internalSort, setInternalSort] = useState<SortDescriptor | undefined>(
    defaultSortDescriptor,
  );
  const activeSort = sortDescriptor ?? internalSort;
  const sortedData = useMemo(
    () => sortRows(data, columns, activeSort),
    [activeSort, columns, data],
  );

  const handleSortChange = (descriptor: SortDescriptor) => {
    if (sortDescriptor === undefined) setInternalSort(descriptor);
    onSortChange?.(descriptor);
  };

  const renderColumn = (column: DataGridColumn<T>, index: number) => {
    const pinnedEdge = isPinnedEdge(columns, index);
    return (
      <Table.Column
        key={column.id}
        id={column.id}
        allowsSorting={column.allowsSorting}
        className={column.headerClassName}
        data-align={column.align}
        data-pinned={column.pinned}
        data-pinned-edge={pinnedEdge || undefined}
        isRowHeader={column.isRowHeader}
        maxWidth={column.maxWidth}
        minWidth={column.minWidth}
        style={getPinnedStyle(columns, index)}
        width={toCssSize(column.width)}
      >
        {({ sortDirection }) => {
          const header =
            typeof column.header === "function" ? column.header({ sortDirection }) : column.header;
          return column.allowsSorting ? (
            <Table.SortableColumnHeader sortDirection={sortDirection}>
              {header}
            </Table.SortableColumnHeader>
          ) : (
            header
          );
        }}
      </Table.Column>
    );
  };

  return (
    <Table.Root
      className={mergeClassNames("data-grid", className)}
      data-heading-height={headingHeight}
      data-row-height={rowHeight}
      data-slot="data-grid"
      data-vertical-align={verticalAlign}
      data-virtualized={virtualized || undefined}
      variant={variant}
    >
      <Table.ScrollContainer className={scrollContainerClassName}>
        <Table.Content
          aria-label={ariaLabel}
          className={contentClassName}
          defaultSelectedKeys={defaultSelectedKeys}
          disabledKeys={disabledKeys}
          dragAndDropHooks={dragAndDropHooks}
          selectedKeys={selectedKeys}
          selectionBehavior={selectionBehavior}
          selectionMode={selectionMode}
          sortDescriptor={activeSort}
          onRowAction={(key) => onRowAction?.(key as string | number)}
          onSelectionChange={onSelectionChange}
          onSortChange={handleSortChange}
        >
          <Table.Header>
            {showSelectionCheckboxes ? (
              <Table.Column id="__selection__" className="data-grid__selection-column" width={52}>
                <Checkbox aria-label="Select all rows" slot="selection" />
              </Table.Column>
            ) : null}
            {columns.map(renderColumn)}
          </Table.Header>
          <Table.Body
            items={sortedData}
            renderEmptyState={
              renderEmptyState
                ? () => (
                    <div className="data-grid__empty-state" data-slot="data-grid-empty-state">
                      {renderEmptyState()}
                    </div>
                  )
                : undefined
            }
          >
            {(item) => (
              <Table.Row id={getRowId(item)} columns={columns}>
                {showSelectionCheckboxes ? (
                  <Table.Cell className="data-grid__selection-cell">
                    <Checkbox aria-label="Select row" slot="selection" />
                  </Table.Cell>
                ) : null}
                {columns.map((column, index) => {
                  const pinnedEdge = isPinnedEdge(columns, index);
                  const value = column.cell
                    ? column.cell(item, column)
                    : column.accessorKey
                      ? (item[column.accessorKey] as ReactNode)
                      : null;
                  return (
                    <Table.Cell
                      key={column.id}
                      className={column.cellClassName}
                      data-align={column.align}
                      data-pinned={column.pinned}
                      data-pinned-edge={pinnedEdge || undefined}
                      style={getPinnedStyle(columns, index)}
                    >
                      {value}
                    </Table.Cell>
                  );
                })}
              </Table.Row>
            )}
          </Table.Body>
        </Table.Content>
      </Table.ScrollContainer>
    </Table.Root>
  );
}

export type { Selection, SortDescriptor, SortDirection };
