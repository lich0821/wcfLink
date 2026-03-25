export namespace main {
	
	export class AccountView {
	    account_id: string;
	    base_url: string;
	    ilink_user_id?: string;
	    enabled: boolean;
	    login_status: string;
	    last_error?: string;
	    last_poll_at?: string;
	    last_inbound_at?: string;
	    created_at: string;
	    updated_at: string;
	
	    static createFrom(source: any = {}) {
	        return new AccountView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.account_id = source["account_id"];
	        this.base_url = source["base_url"];
	        this.ilink_user_id = source["ilink_user_id"];
	        this.enabled = source["enabled"];
	        this.login_status = source["login_status"];
	        this.last_error = source["last_error"];
	        this.last_poll_at = source["last_poll_at"];
	        this.last_inbound_at = source["last_inbound_at"];
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	    }
	}
	export class EventView {
	    id: number;
	    account_id: string;
	    direction: string;
	    event_type: string;
	    from_user_id?: string;
	    to_user_id?: string;
	    message_id?: number;
	    context_token?: string;
	    body_text?: string;
	    raw_json: string;
	    created_at: string;
	
	    static createFrom(source: any = {}) {
	        return new EventView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.account_id = source["account_id"];
	        this.direction = source["direction"];
	        this.event_type = source["event_type"];
	        this.from_user_id = source["from_user_id"];
	        this.to_user_id = source["to_user_id"];
	        this.message_id = source["message_id"];
	        this.context_token = source["context_token"];
	        this.body_text = source["body_text"];
	        this.raw_json = source["raw_json"];
	        this.created_at = source["created_at"];
	    }
	}
	export class LoginSessionView {
	    session_id: string;
	    base_url: string;
	    qr_code_url: string;
	    status: string;
	    account_id?: string;
	    ilink_user_id?: string;
	    error?: string;
	    started_at: string;
	    updated_at: string;
	    completed_at?: string;
	
	    static createFrom(source: any = {}) {
	        return new LoginSessionView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.session_id = source["session_id"];
	        this.base_url = source["base_url"];
	        this.qr_code_url = source["qr_code_url"];
	        this.status = source["status"];
	        this.account_id = source["account_id"];
	        this.ilink_user_id = source["ilink_user_id"];
	        this.error = source["error"];
	        this.started_at = source["started_at"];
	        this.updated_at = source["updated_at"];
	        this.completed_at = source["completed_at"];
	    }
	}
	export class Overview {
	    settings: model.Settings;
	    accounts: AccountView[];
	    connected?: AccountView;
	    needs_login: boolean;
	    suggested_target?: string;
	
	    static createFrom(source: any = {}) {
	        return new Overview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.settings = this.convertValues(source["settings"], model.Settings);
	        this.accounts = this.convertValues(source["accounts"], AccountView);
	        this.connected = this.convertValues(source["connected"], AccountView);
	        this.needs_login = source["needs_login"];
	        this.suggested_target = source["suggested_target"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class WebhookDeadLetterView {
	    id: number;
	    event_id: number;
	    account_id: string;
	    event_type: string;
	    from_user_id?: string;
	    to_user_id?: string;
	    message_id?: number;
	    body_text?: string;
	    status: string;
	    attempt_count: number;
	    max_attempts: number;
	    last_error?: string;
	    last_http_status?: number;
	    last_webhook_url?: string;
	    next_attempt_at: string;
	    last_attempt_at?: string;
	    dead_letter_at?: string;
	    created_at: string;
	    updated_at: string;
	
	    static createFrom(source: any = {}) {
	        return new WebhookDeadLetterView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.event_id = source["event_id"];
	        this.account_id = source["account_id"];
	        this.event_type = source["event_type"];
	        this.from_user_id = source["from_user_id"];
	        this.to_user_id = source["to_user_id"];
	        this.message_id = source["message_id"];
	        this.body_text = source["body_text"];
	        this.status = source["status"];
	        this.attempt_count = source["attempt_count"];
	        this.max_attempts = source["max_attempts"];
	        this.last_error = source["last_error"];
	        this.last_http_status = source["last_http_status"];
	        this.last_webhook_url = source["last_webhook_url"];
	        this.next_attempt_at = source["next_attempt_at"];
	        this.last_attempt_at = source["last_attempt_at"];
	        this.dead_letter_at = source["dead_letter_at"];
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	    }
	}

}

export namespace model {
	
	export class Settings {
	    listen_addr: string;
	    webhook_url: string;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.listen_addr = source["listen_addr"];
	        this.webhook_url = source["webhook_url"];
	    }
	}

}

