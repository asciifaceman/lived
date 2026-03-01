import { FormEvent } from "react";
import { ChatChannel, ChatMessage } from "../api";

type ChatViewProps = {
  channels: ChatChannel[];
  selectedChannel: string;
  onSelectChannel: (channel: string) => void;
  messages: ChatMessage[];
  draft: string;
  onDraftChange: (value: string) => void;
  onSubmit: (event: FormEvent) => void;
  disabled?: boolean;
};

export function ChatView(props: ChatViewProps) {
  const selectedChannelMeta = props.channels.find((entry) => entry.key === props.selectedChannel);

  return (
    <section className="panel chat-layout">
      <aside className="channels-pane">
        <h3>Channels</h3>
        {props.channels.map((channel) => (
          <button
            key={channel.key}
            className={props.selectedChannel === channel.key ? "active" : ""}
            type="button"
            onClick={() => props.onSelectChannel(channel.key)}
          >
            <span>{channel.name} ({channel.key})</span>
            {channel.subject ? <small className="channel-subject">{channel.subject}</small> : null}
          </button>
        ))}
      </aside>

      <div className="chat-pane">
        <h3>#{props.selectedChannel}</h3>
        {selectedChannelMeta?.subject ? <p className="muted channel-header-subject">{selectedChannelMeta.subject}</p> : null}
        <div className="messages-pane">
          {props.messages.map((message) => (
            <div key={message.id} className={`message ${message.messageClass}`}>
              <div className="meta">{message.clock} · {message.author} · {message.messageClass}{message.censored ? ` · censored (${message.censorHits})` : ""}</div>
              <div>{message.message}</div>
            </div>
          ))}
        </div>

        <form onSubmit={props.onSubmit} className="chat-compose">
          <input
            value={props.draft}
            onChange={(event) => props.onDraftChange(event.target.value)}
            maxLength={280}
            placeholder="Type a message"
          />
          <button type="submit" disabled={props.disabled || !props.draft.trim()}>Send</button>
        </form>
      </div>
    </section>
  );
}
