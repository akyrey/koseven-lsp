<?php defined('SYSPATH') or die('No direct script access.');

class Controller_Pages extends Controller {

    public function action_about()
    {
        $user = ORM::factory('User')->find(1);
        $view = View::factory('pages/about')
            ->set('user', $user)
            ->set('show_contact', TRUE)
            ->set('email', 'hello@example.com');
        $this->response->body($view);
    }

    public function action_about_new()
    {
        $user = ORM::factory('User')->find(1);
        $view = new View('pages/about', [
            'user'         => $user,
            'show_contact' => FALSE,
            'email'        => '',
        ]);
        $this->response->body($view);
    }
}
