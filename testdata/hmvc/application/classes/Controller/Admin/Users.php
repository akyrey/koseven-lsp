<?php defined('SYSPATH') or die('No direct script access.');

class Controller_Admin_Users extends Controller {

    public function action_index()
    {
        $this->response->body('Admin users index');
    }

    public function action_edit()
    {
        $this->response->body('Admin users edit');
    }
}
