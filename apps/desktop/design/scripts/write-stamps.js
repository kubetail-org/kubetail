// Sets the stamp on each component and screen in STAMPS. A component's goes
// in its description, keeping the prose above it; a screen's in its section's
// Stamp text layer.
// Fill STAMPS in, then run it through the Figma MCP's use_figma on the Desktop
// file. use_figma runs this as a function body, so it awaits and returns at
// the top.

const STAMPS = [
  // { node: '9:148', stamp: { kubetail: 1, id: 'dashboard/status-pill' } },
  // { node: '2050:2384', stamp: { kubetail: 1, id: 'chat/sandbox-popover/menu', changes: ['The popup is w-100.'] } },
  // { node: '42:10242', stamp: { kubetail: 1, id: 'screens/dashboard-pods' } },
];

function stampComponent(n, json) {
  // descriptionMarkdown, both ways: description stores `<` and `'` as HTML
  // entities and drops the backticks around JSX.
  const text = n.descriptionMarkdown;
  const start = text.search(/(`{3}json\s*)?\{\s*"kubetail"/);
  const prose = (start === -1 ? text : text.slice(0, start)).trimEnd();
  n.descriptionMarkdown = `${prose}\n\n\`\`\`json\n${json}\n\`\`\``.trimStart();
}

// A new Stamp layer goes above the section's top left layer, in the margin
// every section leaves there.
async function stampScreen(section, json) {
  let text = section.children.find((c) => c.type === 'TEXT' && c.name === 'Stamp');
  if (text) {
    await figma.loadFontAsync(text.fontName);
  } else {
    const drawn = section.children;
    await figma.loadFontAsync({ family: 'Inter', style: 'Regular' });
    text = figma.createText();
    text.name = 'Stamp';
    text.fontSize = 12;
    // Locked so a person editing the canvas does not move or delete it. The
    // plugin API still writes it.
    text.locked = true;
    section.appendChild(text);
    text.x = Math.min(...drawn.map((c) => c.x));
    text.y = Math.max(8, Math.min(...drawn.map((c) => c.y)) - 48);
  }
  text.characters = json;
}

const written = [];
for (const { node, stamp } of STAMPS) {
  const n = await figma.getNodeByIdAsync(node);
  const json = JSON.stringify(stamp);
  if (n?.type === 'COMPONENT' || n?.type === 'COMPONENT_SET') {
    stampComponent(n, json);
  } else if (n?.type === 'SECTION') {
    await stampScreen(n, json);
  } else {
    throw new Error(`${node} is not a component or a screen's section`);
  }
  written.push({ node: n.id, name: n.name, id: stamp.id });
}
return written;
