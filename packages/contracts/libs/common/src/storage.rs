use soroban_sdk::{symbol_short, Symbol};

pub fn admin_key() -> Symbol {
    symbol_short!("admin")
}

pub fn balance_key(_account: &Symbol) -> Symbol {
    symbol_short!("bal")
}

pub fn strategy_key(_id: &u32) -> Symbol {
    symbol_short!("strat")
}

pub fn role_key(_account: &Symbol, _role: &Symbol) -> Symbol {
    symbol_short!("role")
}

pub fn initialized_key() -> Symbol {
    symbol_short!("init")
}

pub fn yield_index_key() -> Symbol {
    symbol_short!("y_idx")
}

pub fn user_yield_index_key() -> Symbol {
    symbol_short!("u_idx")
}

pub fn user_accrued_key() -> Symbol {
    symbol_short!("u_acc")
}
